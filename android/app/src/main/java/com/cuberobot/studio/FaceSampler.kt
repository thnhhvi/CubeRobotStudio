package com.cuberobot.studio

import android.graphics.Bitmap
import android.graphics.Color
import android.graphics.PointF
import android.util.Base64
import java.io.ByteArrayOutputStream
import kotlin.math.max
import kotlin.math.min
import kotlin.math.sqrt

/** Result of sampling one cube face. tiles are row-major from overlay corner 1. */
data class FaceSample(
    val tiles: Array<IntArray>,      // 9 x [r,g,b]
    val centerRing: IntArray,        // centre sticker colour measured around the logo
    val logoDetected: Boolean,
    val logoScore: Float,
    val thumbJpegB64: String?,
    val preview: IntArray,           // 9 ARGB colours for the live overlay
    val occluded: BooleanArray       // sticker mostly covered by a black gripper
)

object FaceSampler {
    private val REF = mapOf(
        'W' to intArrayOf(235, 235, 235), 'Y' to intArrayOf(240, 220, 40),
        'R' to intArrayOf(200, 20, 30), 'O' to intArrayOf(250, 120, 20),
        'B' to intArrayOf(20, 70, 200), 'G' to intArrayOf(20, 170, 70)
    )

    /**
     * @param bmp upright camera frame
     * @param viewW/viewH size of the PreviewView (FILL_CENTER scaling)
     * @param at bilinear quad function in *view* pixels
     */
    fun sample(bmp: Bitmap, viewW: Int, viewH: Int, at: (Float, Float) -> PointF,
               withThumb: Boolean): FaceSample {
        val scale = max(viewW.toFloat() / bmp.width, viewH.toFloat() / bmp.height)
        val offX = (viewW - bmp.width * scale) / 2f
        val offY = (viewH - bmp.height * scale) / 2f
        fun px(u: Float, v: Float): Int {
            val p = at(u, v)
            val x = ((p.x - offX) / scale).toInt().coerceIn(0, bmp.width - 1)
            val y = ((p.y - offY) / scale).toInt().coerceIn(0, bmp.height - 1)
            return bmp.getPixel(x, y)
        }

        val tiles = Array(9) { IntArray(3) }
        val preview = IntArray(9)
        val occluded = BooleanArray(9)
        val n = 9
        val hsv = FloatArray(3)
        for (r in 0..2) for (c in 0..2) {
            // robust mean: sample a 9x9 lattice in the inner 50% of the tile, leave out
            // black gripper pixels (an external object), drop the 20% darkest/brightest
            val samples = ArrayList<Int>(n * n)
            var all = 0
            for (i in 0 until n) for (j in 0 until n) {
                val u = (c + 0.25f + 0.5f * i / (n - 1)) / 3f
                val v = (r + 0.25f + 0.5f * j / (n - 1)) / 3f
                val p = px(u, v)
                all++
                Color.colorToHSV(p, hsv)
                if (isGripper(hsv)) continue
                samples.add(p)
            }
            val k = r * 3 + c
            occluded[k] = samples.size < all * MIN_VISIBLE
            val rgb = trimmedMean(samples)
            tiles[k] = rgb
            preview[k] = if (occluded[k]) Color.rgb(60, 60, 60) else nearestColor(rgb)
        }

        // --- centre: GAN logo check (blue mark on the white centre cap). A disc is sampled
        // so the result is the same whichever way the logo faces (also on a 45-degree twist).
        var white = 0; var blue = 0; var total = 0
        val ring = ArrayList<Int>()
        val m = 25
        for (i in 0 until m) for (j in 0 until m) {
            val fu = i / (m - 1f); val fv = j / (m - 1f)
            val dist = sqrt((fu - 0.5f) * (fu - 0.5f) + (fv - 0.5f) * (fv - 0.5f))
            if (dist > 0.42f) continue
            val p = px((1f + fu) / 3f, (1f + fv) / 3f)
            val hsv = FloatArray(3)
            Color.colorToHSV(p, hsv)
            total++
            if (hsv[1] < 0.28f && hsv[2] > 0.45f) white++
            if (hsv[0] in 190f..265f && hsv[1] > 0.35f && hsv[2] > 0.18f) blue++
            if (dist > 0.30f) ring.add(p)
        }
        val wf = white.toFloat() / total
        val bf = blue.toFloat() / total
        // a plain blue centre is >60% blue; the GAN cap is mostly white with a blue mark
        val logo = wf >= 0.30f && bf in 0.03f..0.55f
        val ringRgb = trimmedMean(ring)
        if (logo) {
            tiles[4] = ringRgb
            preview[4] = Color.WHITE
        }

        val thumb = if (withThumb) thumbnail { u, v -> px(u, v) } else null
        if (logo) occluded[4] = false
        return FaceSample(tiles, ringRgb, logo, wf * (if (logo) 1f else 0f) + bf, thumb, preview, occluded)
    }

    /** a sticker needs this share of non-gripper pixels to be read (same as the laptop) */
    private const val MIN_VISIBLE = 0.40f

    /** black / very dark grey plastic; coloured stickers stay saturated even in shadow */
    private fun isGripper(hsv: FloatArray) = hsv[2] < 0.20f || (hsv[2] < 0.36f && hsv[1] < 0.35f)

    private fun trimmedMean(px: List<Int>): IntArray {
        if (px.isEmpty()) return intArrayOf(0, 0, 0)
        val sorted = px.sortedBy { Color.red(it) + Color.green(it) + Color.blue(it) }
        val cut = sorted.size / 5
        val keep = sorted.subList(cut, max(cut + 1, sorted.size - cut))
        var r = 0L; var g = 0L; var b = 0L
        for (p in keep) { r += Color.red(p); g += Color.green(p); b += Color.blue(p) }
        val k = keep.size
        return intArrayOf((r / k).toInt(), (g / k).toInt(), (b / k).toInt())
    }

    private fun nearestColor(rgb: IntArray): Int {
        // quick hue-based guess, only used for the on-screen preview
        val hsv = FloatArray(3)
        Color.RGBToHSV(rgb[0], rgb[1], rgb[2], hsv)
        val key = when {
            hsv[1] < 0.25f && hsv[2] > 0.4f -> 'W'
            hsv[0] < 12f || hsv[0] >= 330f -> 'R'
            hsv[0] < 40f -> 'O'
            hsv[0] < 75f -> 'Y'
            hsv[0] < 170f -> 'G'
            hsv[0] < 270f -> 'B'
            else -> 'R'
        }
        val c = REF[key]!!
        return Color.rgb(c[0], c[1], c[2])
    }

    private fun thumbnail(px: (Float, Float) -> Int): String {
        val s = 96
        val out = Bitmap.createBitmap(s, s, Bitmap.Config.ARGB_8888)
        for (y in 0 until s) for (x in 0 until s) {
            out.setPixel(x, y, px((x + 0.5f) / s, (y + 0.5f) / s))
        }
        val bos = ByteArrayOutputStream()
        out.compress(Bitmap.CompressFormat.JPEG, 70, bos)
        out.recycle()
        return Base64.encodeToString(bos.toByteArray(), Base64.NO_WRAP)
    }

    @Suppress("unused")
    private fun clamp(v: Int) = min(255, max(0, v))
}
