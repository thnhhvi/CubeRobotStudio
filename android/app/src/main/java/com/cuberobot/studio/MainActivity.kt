package com.cuberobot.studio

import android.Manifest
import android.annotation.SuppressLint
import android.content.pm.PackageManager
import android.graphics.Bitmap
import android.graphics.Color
import android.graphics.Matrix
import android.graphics.Typeface
import android.graphics.drawable.GradientDrawable
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.text.InputType
import android.view.Gravity
import android.view.View
import android.view.ViewGroup.LayoutParams.MATCH_PARENT
import android.view.ViewGroup.LayoutParams.WRAP_CONTENT
import android.webkit.WebChromeClient
import android.webkit.WebView
import android.webkit.WebViewClient
import android.widget.Button
import android.widget.EditText
import android.widget.FrameLayout
import android.widget.LinearLayout
import android.widget.TextView
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AppCompatActivity
import androidx.camera.core.CameraSelector
import androidx.camera.core.ImageAnalysis
import androidx.camera.core.ImageProxy
import androidx.camera.core.Preview
import androidx.camera.lifecycle.ProcessCameraProvider
import androidx.camera.view.PreviewView
import androidx.core.content.ContextCompat
import androidx.core.view.ViewCompat
import androidx.core.view.WindowInsetsCompat
import com.journeyapps.barcodescanner.ScanContract
import com.journeyapps.barcodescanner.ScanOptions
import org.json.JSONArray
import org.json.JSONObject
import java.util.concurrent.Executors

/**
 * Phone side of Cube Robot Studio.
 *  - Camera screen with a draggable 3x3 grid: captures each face the robot
 *    presents and sends the colour samples to the laptop.
 *  - Otherwise shows the laptop's live 3D model + robot controls (WebView).
 */
class MainActivity : AppCompatActivity(), Link.Listener {
    companion object {
        const val GRAD = "gradient"
        const val VERSION = "2.4"
    }
    private var loadedHost: String? = null

    private lateinit var link: Link
    private lateinit var status: TextView
    private lateinit var web: WebView
    private lateinit var camLayer: FrameLayout
    private lateinit var previewView: PreviewView
    private lateinit var overlay: QuadOverlay
    private lateinit var prompt: TextView
    private lateinit var captureBtn: Button
    private lateinit var resetBtn: Button
    private lateinit var retakeBtn: Button
    private lateinit var confirmBtn: Button
    @Volatile private var reviewing = false
    @Volatile private var scanReady = false
    private lateinit var connectPanel: LinearLayout
    private lateinit var connectHolder: View
    private lateinit var hostEdit: EditText
    private lateinit var camToggle: Button

    private val ui = Handler(Looper.getMainLooper())
    private val analysisExec = Executors.newSingleThreadExecutor()
    private var cameraProvider: ProcessCameraProvider? = null
    @Volatile private var latest: Bitmap? = null
    @Volatile private var lastAnalysis = 0L
    @Volatile private var cameraWanted = false

    // scan prompt state
    @Volatile private var waitingCapture = false
    private var promptIndex = -1
    private var userCamera = false

    private val prefs by lazy { getSharedPreferences("cuberobot", MODE_PRIVATE) }

    private val qrScan = registerForActivityResult(ScanContract()) { result ->
        val txt = result.contents
        if (txt != null) link.connectWithQr(txt)
    }

    private fun scanQr() {
        qrScan.launch(ScanOptions()
            .setDesiredBarcodeFormats(ScanOptions.QR_CODE)
            .setPrompt("Scan the QR code shown in Cube Robot Studio on the laptop")
            .setBeepEnabled(false)
            .setOrientationLocked(false))
    }

    private val camPermission = registerForActivityResult(ActivityResultContracts.RequestPermission()) { ok ->
        if (ok) startCamera() else setStatus("Camera permission is needed to scan")
    }

    // ------------------------------------------------------------------ UI
    @SuppressLint("SetJavaScriptEnabled")
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val dark = Color.parseColor("#12101f")
        val panel = Color.parseColor("#1b1830")

        val root = LinearLayout(this).apply { orientation = LinearLayout.VERTICAL; setBackgroundColor(dark) }

        // top bar
        val bar = LinearLayout(this).apply {
            orientation = LinearLayout.HORIZONTAL
            setBackgroundColor(panel)
            setPadding(dp(12), dp(8), dp(8), dp(8))
            gravity = Gravity.CENTER_VERTICAL
        }
        status = TextView(this).apply { setTextColor(Color.WHITE); textSize = 13f; text = "Cube Robot v$VERSION" }
        camToggle = smallButton("Camera") {
            userCamera = !userCamera
            showCamera(userCamera || waitingCapture)
        }
        val reconnect = smallButton("Link") { showConnectPanel(true) }
        bar.addView(status, LinearLayout.LayoutParams(0, WRAP_CONTENT, 1f))
        bar.addView(camToggle)
        bar.addView(reconnect)
        root.addView(bar, LinearLayout.LayoutParams(MATCH_PARENT, WRAP_CONTENT))

        val content = FrameLayout(this)
        root.addView(content, LinearLayout.LayoutParams(MATCH_PARENT, 0, 1f))

        // WebView with the live 3D model and the robot buttons (served by the laptop)
        web = WebView(this).apply {
            settings.javaScriptEnabled = true
            settings.domStorageEnabled = true
            settings.allowFileAccess = true
            setBackgroundColor(dark)
            webViewClient = WebViewClient()
            webChromeClient = WebChromeClient()
        }
        content.addView(web, FrameLayout.LayoutParams(MATCH_PARENT, MATCH_PARENT))

        // camera layer
        camLayer = FrameLayout(this).apply { visibility = View.GONE; setBackgroundColor(Color.BLACK) }
        val camColumn = LinearLayout(this).apply { orientation = LinearLayout.VERTICAL }
        val previewBox = FrameLayout(this)
        previewView = PreviewView(this).apply { scaleType = PreviewView.ScaleType.FILL_CENTER }
        overlay = QuadOverlay(this).apply {
            load(prefs)
            onChanged = { save(prefs) }
        }
        previewBox.addView(previewView, FrameLayout.LayoutParams(MATCH_PARENT, MATCH_PARENT))
        previewBox.addView(overlay, FrameLayout.LayoutParams(MATCH_PARENT, MATCH_PARENT))
        val camPanel = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setBackgroundColor(Color.parseColor("#1b1830"))
            setPadding(dp(12), dp(8), dp(12), dp(10))
        }
        prompt = TextView(this).apply {
            setTextColor(Color.WHITE); textSize = 16f; typeface = Typeface.DEFAULT_BOLD
            text = "Drag the 4 blue corners onto the face the robot shows."
        }
        val row = LinearLayout(this).apply { orientation = LinearLayout.HORIZONTAL }
        captureBtn = bigButton("📷 Scan face", GRAD) { if (scanReady) beginScan() else captureNow() }
        resetBtn = bigButton("Reset grid", "#24203d") { overlay.resetSquare() }
        retakeBtn = bigButton("↺ Retake", "#24203d") { answerReview("retake") }
        confirmBtn = bigButton("Confirm ✓ next face", GRAD) { answerReview("confirm") }
        row.addView(resetBtn, LinearLayout.LayoutParams(0, dp(52), 1f).apply { rightMargin = dp(8) })
        row.addView(captureBtn, LinearLayout.LayoutParams(0, dp(52), 2f))
        row.addView(retakeBtn, LinearLayout.LayoutParams(0, dp(52), 1f).apply { rightMargin = dp(8) })
        row.addView(confirmBtn, LinearLayout.LayoutParams(0, dp(52), 2f))
        retakeBtn.visibility = View.GONE
        confirmBtn.visibility = View.GONE
        camPanel.addView(prompt)
        camPanel.addView(row, LinearLayout.LayoutParams(MATCH_PARENT, WRAP_CONTENT).apply { topMargin = dp(8) })
        // camera picture on top, prompt + buttons underneath (they no longer cover the grid)
        camColumn.addView(previewBox, LinearLayout.LayoutParams(MATCH_PARENT, 0, 1f))
        camColumn.addView(camPanel, LinearLayout.LayoutParams(MATCH_PARENT, WRAP_CONTENT))
        camLayer.addView(camColumn, FrameLayout.LayoutParams(MATCH_PARENT, MATCH_PARENT))
        content.addView(camLayer, FrameLayout.LayoutParams(MATCH_PARENT, MATCH_PARENT))

        // connect panel
        connectPanel = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setBackgroundColor(dark)
            setPadding(dp(20), dp(24), dp(20), dp(20))
        }
        val title = TextView(this).apply {
            text = "Connect to Cube Robot Studio  v$VERSION"; setTextColor(Color.WHITE); textSize = 22f
            typeface = Typeface.DEFAULT_BOLD
        }
        val help = TextView(this).apply {
            setTextColor(Color.parseColor("#a29fc2")); textSize = 14f
            text = "Open \"Pair a phone\" in Cube Robot Studio on the laptop, then scan its QR code " +
                "or type the 6-digit code.\n\nPhone and laptop on the same Wi-Fi, or a USB cable with " +
                "USB debugging on. (Bluetooth tethering also works.)"
        }
        val qrBtn = bigButton("Scan QR code", GRAD) { scanQr() }
        val orText = TextView(this).apply {
            setTextColor(Color.parseColor("#a29fc2")); textSize = 13f
            text = "or type the 6-digit code (or the laptop address):"
        }
        hostEdit = EditText(this).apply {
            hint = "123 456"
            setTextColor(Color.WHITE); setHintTextColor(Color.GRAY)
            textSize = 26f
            inputType = InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_VARIATION_URI
        }
        val go = bigButton("Connect", "#24203d") { connectFromInput() }
        val auto = bigButton("Search automatically", "#24203d") { link.autoConnect(prefs.getString("host", null)) }
        connectPanel.addView(title)
        connectPanel.addView(help, LinearLayout.LayoutParams(MATCH_PARENT, WRAP_CONTENT).apply { topMargin = dp(12) })
        connectPanel.addView(qrBtn, LinearLayout.LayoutParams(MATCH_PARENT, dp(56)).apply { topMargin = dp(20) })
        connectPanel.addView(orText, LinearLayout.LayoutParams(MATCH_PARENT, WRAP_CONTENT).apply { topMargin = dp(20) })
        connectPanel.addView(hostEdit)
        connectPanel.addView(go, LinearLayout.LayoutParams(MATCH_PARENT, dp(52)).apply { topMargin = dp(8) })
        connectPanel.addView(auto, LinearLayout.LayoutParams(MATCH_PARENT, dp(52)).apply { topMargin = dp(8) })
        val connectScroll = android.widget.ScrollView(this).apply {
            setBackgroundColor(dark)
            isFillViewport = true
            addView(connectPanel)
        }
        connectHolder = connectScroll
        content.addView(connectScroll, FrameLayout.LayoutParams(MATCH_PARENT, MATCH_PARENT))

        setContentView(root)
        // Android 15 draws apps edge-to-edge: pad the whole UI by the system bars, display
        // cutout and keyboard so the top bar (Camera / Link) is never under the status bar.
        ViewCompat.setOnApplyWindowInsetsListener(root) { v, insets ->
            val bars = insets.getInsets(WindowInsetsCompat.Type.systemBars() or WindowInsetsCompat.Type.displayCutout())
            val ime = insets.getInsets(WindowInsetsCompat.Type.ime())
            v.setPadding(bars.left, bars.top, bars.right, maxOf(bars.bottom, ime.bottom))
            WindowInsetsCompat.CONSUMED
        }

        link = Link(this)
        link.listener = this
        link.autoConnect(prefs.getString("host", null))

        if (ContextCompat.checkSelfPermission(this, Manifest.permission.CAMERA) != PackageManager.PERMISSION_GRANTED) {
            camPermission.launch(Manifest.permission.CAMERA)
        }
    }

    override fun onDestroy() {
        super.onDestroy()
        link.stop()
        analysisExec.shutdown()
    }

    private fun connectFromInput() {
        val raw = hostEdit.text.toString().trim()
        val digits = raw.filter { it.isDigit() }
        when {
            raw.isEmpty() -> setStatus("Type the 6-digit code shown on the laptop")
            digits.length == 6 && !raw.contains('.') -> link.connectWithCode(digits)
            else -> link.connectTo(raw)
        }
    }

    private fun colorOf(c: String): Int = when (c) {
        "W" -> Color.rgb(235, 235, 235); "Y" -> Color.rgb(240, 220, 40); "R" -> Color.rgb(200, 20, 30)
        "O" -> Color.rgb(250, 120, 20); "B" -> Color.rgb(20, 70, 200); "G" -> Color.rgb(20, 170, 70)
        else -> Color.GRAY
    }

    private fun dp(v: Int) = (v * resources.displayMetrics.density).toInt()

    private fun smallButton(label: String, onClick: () -> Unit) = Button(this).apply {
        text = label; isAllCaps = false; textSize = 13f
        setTextColor(Color.WHITE)
        background = GradientDrawable().apply {
            setColor(Color.parseColor("#24203d")); cornerRadius = dp(10).toFloat()
        }
        setOnClickListener { onClick() }
        minWidth = dp(64); minimumWidth = dp(64)
    }

    private fun bigButton(label: String, color: String, onClick: () -> Unit) = Button(this).apply {
        text = label; isAllCaps = false; textSize = 16f; typeface = Typeface.DEFAULT_BOLD
        setTextColor(Color.WHITE)
        background = if (color == GRAD) {
            // purple-blue accent gradient #977DFF -> #0600AB
            GradientDrawable(GradientDrawable.Orientation.TL_BR,
                intArrayOf(Color.parseColor("#977DFF"), Color.parseColor("#0600AB"))).apply {
                cornerRadius = dp(12).toFloat()
            }
        } else {
            GradientDrawable().apply { setColor(Color.parseColor(color)); cornerRadius = dp(12).toFloat() }
        }
        setOnClickListener { onClick() }
    }

    private fun setStatus(s: String) = ui.post { status.text = s }

    private fun showConnectPanel(show: Boolean) = ui.post {
        connectHolder.visibility = if (show) View.VISIBLE else View.GONE
    }

    private fun showCamera(show: Boolean) = ui.post {
        camLayer.visibility = if (show) View.VISIBLE else View.GONE
        camToggle.text = if (show) "3D view" else "Camera"
        cameraWanted = show
        if (show) {
            if (ContextCompat.checkSelfPermission(this, Manifest.permission.CAMERA) == PackageManager.PERMISSION_GRANTED)
                startCamera()
            else camPermission.launch(Manifest.permission.CAMERA)
        } else stopCamera()
    }

    // ------------------------------------------------------------------ camera
    private fun startCamera() {
        if (!cameraWanted) return
        val future = ProcessCameraProvider.getInstance(this)
        future.addListener({
            val provider = future.get()
            cameraProvider = provider
            val preview = Preview.Builder().build().also { it.setSurfaceProvider(previewView.surfaceProvider) }
            val analysis = ImageAnalysis.Builder()
                .setBackpressureStrategy(ImageAnalysis.STRATEGY_KEEP_ONLY_LATEST)
                .setOutputImageFormat(ImageAnalysis.OUTPUT_IMAGE_FORMAT_RGBA_8888)
                .build()
            analysis.setAnalyzer(analysisExec) { proxy -> analyze(proxy) }
            try {
                provider.unbindAll()
                provider.bindToLifecycle(this, CameraSelector.DEFAULT_BACK_CAMERA, preview, analysis)
            } catch (e: Exception) {
                setStatus("Camera error: ${e.message}")
            }
        }, ContextCompat.getMainExecutor(this))
    }

    private fun stopCamera() {
        cameraProvider?.unbindAll()
    }

    private fun analyze(proxy: ImageProxy) {
        try {
            val now = System.currentTimeMillis()
            if (now - lastAnalysis < 180) return
            lastAnalysis = now
            val raw = proxy.toBitmap()
            val rot = proxy.imageInfo.rotationDegrees
            val upright = if (rot != 0) {
                val m = Matrix().apply { postRotate(rot.toFloat()) }
                Bitmap.createBitmap(raw, 0, 0, raw.width, raw.height, m, true)
            } else raw
            latest = upright
            if (overlay.width > 0 && !reviewing) {
                val s = FaceSampler.sample(upright, previewView.width, previewView.height,
                    { u, v -> overlay.at(u, v) }, false)
                ui.post {
                    overlay.liveColors = s.preview
                    overlay.logoSeen = s.logoDetected
                    overlay.hidden = s.occluded
                    overlay.invalidate()
                }
            }
        } catch (_: Exception) {
        } finally {
            proxy.close()
        }
    }

    /** Scan mode armed: wait for the user to load the cube and press Start scanning. */
    private fun setReady(on: Boolean, front: String = "", top: String = "") {
        scanReady = on
        if (on) {
            setReview(false)
            waitingCapture = false
            showCamera(true)
            captureBtn.text = "Start scanning ▶"
            captureBtn.isEnabled = true
            prompt.text = "Scan mode: clamp the cube in the robot (any way round - the centre colours are " +
                "found from the first two faces), fit the 4 blue corners to where the robot shows the face, " +
                "then tap Start scanning."
        } else {
            captureBtn.text = "📷 Scan face"
        }
    }

    private fun beginScan() {
        captureBtn.isEnabled = false
        link.post("/api/scan/begin", JSONObject()) { res ->
            ui.post {
                captureBtn.isEnabled = true
                if (res != null && res.optBoolean("ok", false)) {
                    setReady(false)
                    prompt.text = "Robot is presenting the first face…"
                } else prompt.text = "Could not start: ${res?.optString("error") ?: "no connection"}"
            }
        }
    }

    /** Review step: freeze the captured colours until the user confirms or retakes. */
    private fun setReview(on: Boolean) {
        reviewing = on
        captureBtn.visibility = if (on) View.GONE else View.VISIBLE
        resetBtn.visibility = if (on) View.GONE else View.VISIBLE
        retakeBtn.visibility = if (on) View.VISIBLE else View.GONE
        confirmBtn.visibility = if (on) View.VISIBLE else View.GONE
        retakeBtn.isEnabled = true
        confirmBtn.isEnabled = true
    }

    private fun answerReview(action: String) {
        retakeBtn.isEnabled = false
        confirmBtn.isEnabled = false
        link.post("/api/scan/$action", JSONObject()) { res ->
            ui.post {
                if (res == null || !res.optBoolean("ok", false)) {
                    retakeBtn.isEnabled = true
                    confirmBtn.isEnabled = true
                    prompt.text = "Could not send: ${res?.optString("error") ?: "no connection"}"
                } else if (action == "confirm") {
                    prompt.text = "Confirmed ✓  (robot is turning to the next face…)"
                } else {
                    prompt.text = "Retaking…"
                }
            }
        }
    }

    private fun captureNow() {
        val bmp = latest
        if (bmp == null) {
            prompt.text = "Camera not ready yet…"
            return
        }
        if (!waitingCapture) {
            prompt.text = "Not scanning right now – start a scan on the laptop (or tap Scan in the 3D view)."
            return
        }
        waitingCapture = false
        captureBtn.isEnabled = false
        val vw = previewView.width
        val vh = previewView.height
        analysisExec.execute {
            val s = FaceSampler.sample(bmp, vw, vh, { u, v -> overlay.at(u, v) }, true)
            val tiles = JSONArray()
            for (t in s.tiles) tiles.put(JSONArray().put(t[0]).put(t[1]).put(t[2]))
            val body = JSONObject()
                .put("tiles", tiles)
                .put("center_ring", JSONArray().put(s.centerRing[0]).put(s.centerRing[1]).put(s.centerRing[2]))
                .put("logo", JSONObject().put("detected", s.logoDetected).put("score", s.logoScore.toDouble()))
                .put("thumb", s.thumbJpegB64)
                .put("occluded", JSONArray().apply { for (o in s.occluded) put(o) })
                .put("index", promptIndex)
            link.post("/api/scan/capture", body) { res ->
                ui.post {
                    captureBtn.isEnabled = true
                    if (res != null && res.optBoolean("ok", false)) {
                        prompt.text = "Captured – checking…"
                    } else {
                        waitingCapture = true
                        prompt.text = "Capture failed: ${res?.optString("error") ?: "no connection"} – tap Scan face"
                    }
                }
            }
        }
    }

    // ------------------------------------------------------------------ link events
    override fun onStatus(connected: Boolean, host: String?, message: String) {
        setStatus(message)
        if (connected && host != null) {
            prefs.edit().putString("host", if (host == "127.0.0.1") prefs.getString("host", "") else host).apply()
            showConnectPanel(false)
            ui.post {
                // the 3D view + buttons are bundled in the APK (assets/phone.html) and run locally;
                // only the robot/solver API calls go to the laptop
                if (loadedHost != host) {
                    loadedHost = host
                    web.loadUrl("file:///android_asset/phone.html#base=http://$host:${Link.PORT}")
                }
            }
        } else if (host == null) {
            showConnectPanel(true)
        }
    }

    override fun onEvent(ev: JSONObject) {
        when (ev.optString("type")) {
            "scan_ready" -> ui.post {
                setReady(true, ev.optString("front"), ev.optString("top"))
            }
            "capture_now" -> ui.post {
                if (waitingCapture && !reviewing) captureNow()
            }
            "scan_prompt" -> ui.post {
                setReady(false)
                setReview(false)
                promptIndex = ev.optInt("index")
                val name = ev.optString("color_name").uppercase()  // colour, or position for the first two
                val total = ev.optInt("total", 6)
                waitingCapture = true
                showCamera(true)
                captureBtn.isEnabled = true
                val hint = ev.optString("manual_hint", "")
                prompt.text = if (ev.optBoolean("second_look", false))
                    "Second look at the $name face (${promptIndex + 1}/$total): other arms holding, " +
                        "capturing the stickers the grippers hid…"
                else "Aim at the $name face  (${promptIndex + 1}/$total), then tap Scan face" +
                    if (hint.isNotEmpty() && hint != "null") "\n$hint" else ""
                overlay.hidden = null
                overlay.invalidate()
            }
            "scan_review" -> ui.post {
                showCamera(true)
                setReview(true)
                val prev = ev.optJSONArray("preview")
                if (prev != null && prev.length() == 9) {
                    overlay.liveColors = IntArray(9) { colorOf(prev.optString(it)) }
                    overlay.logoSeen = ev.optBoolean("logo", false)
                    val hid = ev.optJSONArray("hidden")
                    overlay.hidden = if (hid != null && hid.length() == 9) BooleanArray(9) { hid.optBoolean(it) } else null
                    overlay.invalidate()
                }
                val name = ev.optString("color_name").uppercase()
                val nHid = ev.optInt("hidden_count", 0)
                prompt.text = "Check the $name face (${ev.optInt("index") + 1}/${ev.optInt("total", 6)}): " +
                    "colours right?" + if (nHid > 0) "\n$nHid sticker(s) hidden by a gripper (X) – worked out from the other faces" else ""
            }
            "scan_done" -> ui.post {
                setReview(false)
                waitingCapture = false
                prompt.text = "Scan complete ✓"
                ui.postDelayed({ if (!waitingCapture) showCamera(userCamera) }, 900)
            }
            "state" -> {
                val scanning = ev.optString("busy_task") == "scan"
                val sc = ev.optJSONObject("scan")
                val phase = sc?.optString("phase") ?: ""
                if (scanning && phase == "ready" && !scanReady) ui.post {
                    val st = ev.optJSONObject("settings")
                    val names = mapOf("W" to "white", "Y" to "yellow", "R" to "red", "O" to "orange",
                        "B" to "blue", "G" to "green")
                    setReady(true, names[st?.optString("front")] ?: "", names[st?.optString("top")] ?: "")
                }
                // joined (or reconnected) while the laptop already waits for a face / a confirm
                if (scanning && phase == "capture" && !waitingCapture && !reviewing && !scanReady) ui.post {
                    val names = mapOf("W" to "white", "Y" to "yellow", "R" to "red", "O" to "orange",
                        "B" to "blue", "G" to "green")
                    promptIndex = sc?.optInt("index") ?: 0
                    waitingCapture = true
                    setReview(false)
                    showCamera(true)
                    captureBtn.isEnabled = true
                    prompt.text = "Aim at the ${(names[sc?.optString("color")] ?: "").uppercase()} face " +
                        "(${promptIndex + 1}/${sc?.optInt("total") ?: 6}), then tap Scan face"
                }
                if (scanning && phase == "confirm" && !reviewing) ui.post {
                    showCamera(true)
                    setReview(true)
                    prompt.text = "Check the colours on the laptop, then Confirm or Retake"
                }
                if (!scanning && (waitingCapture || reviewing || scanReady)) ui.post {
                    waitingCapture = false
                    setReady(false)
                    setReview(false)
                    showCamera(userCamera)
                }
            }
        }
    }
}
