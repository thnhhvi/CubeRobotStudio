package com.cuberobot.studio

import android.annotation.SuppressLint
import android.content.Context
import android.graphics.Canvas
import android.graphics.Color
import android.graphics.Paint
import android.graphics.Path
import android.graphics.PointF
import android.view.MotionEvent
import android.view.View
import kotlin.math.hypot

/**
 * Four draggable corners that the user fits onto the cube face once (the
 * robot always presents faces at the same spot). The 3x3 grid is the bilinear
 * subdivision of that quad, so a face seen at an angle (45 deg tilt) works.
 * Corners are stored normalised (0..1) in the view.
 */
class QuadOverlay(ctx: Context) : View(ctx) {
    /** normalised corners: 0=top-left 1=top-right 2=bottom-right 3=bottom-left */
    val corners = arrayOf(PointF(0.2f, 0.3f), PointF(0.8f, 0.3f), PointF(0.8f, 0.7f), PointF(0.2f, 0.7f))
    var liveColors: IntArray? = null
    var logoSeen = false
    var onChanged: (() -> Unit)? = null

    private var dragIdx = -1
    private val gridPaint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
        color = Color.WHITE; strokeWidth = 3f; style = Paint.Style.STROKE; alpha = 210
    }
    private val samplePaint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
        color = Color.YELLOW; strokeWidth = 2f; style = Paint.Style.STROKE; alpha = 200
    }
    private val handlePaint = Paint(Paint.ANTI_ALIAS_FLAG).apply { color = Color.parseColor("#3a7bf0") }
    private val textPaint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
        color = Color.WHITE; textSize = 34f; textAlign = Paint.Align.CENTER; isFakeBoldText = true
    }
    private val fillPaint = Paint(Paint.ANTI_ALIAS_FLAG)

    fun px(i: Int) = PointF(corners[i].x * width, corners[i].y * height)

    /** bilinear point inside the quad, u,v in 0..1 (view pixels) */
    fun at(u: Float, v: Float): PointF {
        val a = px(0); val b = px(1); val c = px(2); val d = px(3)
        val topX = a.x + (b.x - a.x) * u; val topY = a.y + (b.y - a.y) * u
        val botX = d.x + (c.x - d.x) * u; val botY = d.y + (c.y - d.y) * u
        return PointF(topX + (botX - topX) * v, topY + (botY - topY) * v)
    }

    override fun onDraw(canvas: Canvas) {
        super.onDraw(canvas)
        if (width == 0) return
        // outline + grid
        for (k in 0..3) {
            val t = k / 3f
            line(canvas, at(t, 0f), at(t, 1f), gridPaint)
            line(canvas, at(0f, t), at(1f, t), gridPaint)
        }
        // sample regions + live colour preview
        for (r in 0..2) for (c in 0..2) {
            val u0 = (c + 0.3f) / 3f; val u1 = (c + 0.7f) / 3f
            val v0 = (r + 0.3f) / 3f; val v1 = (r + 0.7f) / 3f
            val p = Path()
            val p0 = at(u0, v0); val p1 = at(u1, v0); val p2 = at(u1, v1); val p3 = at(u0, v1)
            p.moveTo(p0.x, p0.y); p.lineTo(p1.x, p1.y); p.lineTo(p2.x, p2.y); p.lineTo(p3.x, p3.y); p.close()
            liveColors?.let {
                fillPaint.color = it[r * 3 + c]
                fillPaint.alpha = 170
                canvas.drawPath(p, fillPaint)
            }
            canvas.drawPath(p, samplePaint)
        }
        if (logoSeen) {
            val m = at(0.5f, 0.5f)
            textPaint.textSize = 26f
            canvas.drawText("GAN ✓", m.x, m.y + 9f, textPaint)
            textPaint.textSize = 34f
        }
        for (i in 0..3) {
            val p = px(i)
            canvas.drawCircle(p.x, p.y, 30f, handlePaint)
            canvas.drawText("${i + 1}", p.x, p.y + 12f, textPaint)
        }
    }

    private fun line(c: Canvas, a: PointF, b: PointF, p: Paint) = c.drawLine(a.x, a.y, b.x, b.y, p)

    @SuppressLint("ClickableViewAccessibility")
    override fun onTouchEvent(e: MotionEvent): Boolean {
        when (e.actionMasked) {
            MotionEvent.ACTION_DOWN -> {
                dragIdx = -1
                var best = 90f
                for (i in 0..3) {
                    val p = px(i)
                    val d = hypot(p.x - e.x, p.y - e.y)
                    if (d < best) { best = d; dragIdx = i }
                }
                return dragIdx >= 0
            }
            MotionEvent.ACTION_MOVE -> if (dragIdx >= 0) {
                corners[dragIdx].x = (e.x / width).coerceIn(0f, 1f)
                corners[dragIdx].y = (e.y / height).coerceIn(0f, 1f)
                invalidate()
                return true
            }
            MotionEvent.ACTION_UP, MotionEvent.ACTION_CANCEL -> if (dragIdx >= 0) {
                dragIdx = -1
                onChanged?.invoke()
                return true
            }
        }
        return false
    }

    fun save(prefs: android.content.SharedPreferences) {
        val e = prefs.edit()
        for (i in 0..3) { e.putFloat("q${i}x", corners[i].x); e.putFloat("q${i}y", corners[i].y) }
        e.apply()
    }

    fun load(prefs: android.content.SharedPreferences) {
        for (i in 0..3) {
            corners[i].x = prefs.getFloat("q${i}x", corners[i].x)
            corners[i].y = prefs.getFloat("q${i}y", corners[i].y)
        }
        invalidate()
    }

    fun resetSquare() {
        val s = 0.3f * height / width.coerceAtLeast(1)
        val cx = 0.5f; val cy = 0.5f
        corners[0].set(cx - s, cy - 0.3f); corners[1].set(cx + s, cy - 0.3f)
        corners[2].set(cx + s, cy + 0.3f); corners[3].set(cx - s, cy + 0.3f)
        invalidate()
        onChanged?.invoke()
    }
}
