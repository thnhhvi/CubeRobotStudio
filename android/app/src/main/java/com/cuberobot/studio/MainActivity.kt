package com.cuberobot.studio

import android.Manifest
import android.annotation.SuppressLint
import android.content.pm.PackageManager
import android.graphics.Bitmap
import android.graphics.Color
import android.graphics.Matrix
import android.graphics.Typeface
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

    private lateinit var link: Link
    private lateinit var status: TextView
    private lateinit var web: WebView
    private lateinit var camLayer: FrameLayout
    private lateinit var previewView: PreviewView
    private lateinit var overlay: QuadOverlay
    private lateinit var prompt: TextView
    private lateinit var captureBtn: Button
    private lateinit var connectPanel: LinearLayout
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
    private var autoCaptureRunnable: Runnable? = null

    private val prefs by lazy { getSharedPreferences("cuberobot", MODE_PRIVATE) }

    private val camPermission = registerForActivityResult(ActivityResultContracts.RequestPermission()) { ok ->
        if (ok) startCamera() else setStatus("Camera permission is needed to scan")
    }

    // ------------------------------------------------------------------ UI
    @SuppressLint("SetJavaScriptEnabled")
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val dark = Color.parseColor("#1f1e1d")
        val panel = Color.parseColor("#2a2927")

        val root = LinearLayout(this).apply { orientation = LinearLayout.VERTICAL; setBackgroundColor(dark) }

        // top bar
        val bar = LinearLayout(this).apply {
            orientation = LinearLayout.HORIZONTAL
            setBackgroundColor(panel)
            setPadding(dp(12), dp(8), dp(8), dp(8))
            gravity = Gravity.CENTER_VERTICAL
        }
        status = TextView(this).apply { setTextColor(Color.WHITE); textSize = 13f; text = "Starting…" }
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
            setBackgroundColor(dark)
            webViewClient = WebViewClient()
            webChromeClient = WebChromeClient()
        }
        content.addView(web, FrameLayout.LayoutParams(MATCH_PARENT, MATCH_PARENT))

        // camera layer
        camLayer = FrameLayout(this).apply { visibility = View.GONE; setBackgroundColor(Color.BLACK) }
        previewView = PreviewView(this).apply { scaleType = PreviewView.ScaleType.FILL_CENTER }
        overlay = QuadOverlay(this).apply {
            load(prefs)
            onChanged = { save(prefs) }
        }
        camLayer.addView(previewView, FrameLayout.LayoutParams(MATCH_PARENT, MATCH_PARENT))
        camLayer.addView(overlay, FrameLayout.LayoutParams(MATCH_PARENT, MATCH_PARENT))
        val camPanel = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setBackgroundColor(Color.argb(200, 31, 30, 29))
            setPadding(dp(14), dp(10), dp(14), dp(14))
        }
        prompt = TextView(this).apply {
            setTextColor(Color.WHITE); textSize = 18f; typeface = Typeface.DEFAULT_BOLD
            text = "Drag the 4 blue corners onto the face the robot shows."
        }
        val row = LinearLayout(this).apply { orientation = LinearLayout.HORIZONTAL }
        captureBtn = bigButton("Capture", "#3fb27f") { captureNow() }
        val resetBtn = bigButton("Reset grid", "#33312e") { overlay.resetSquare() }
        row.addView(resetBtn, LinearLayout.LayoutParams(0, dp(52), 1f).apply { rightMargin = dp(8) })
        row.addView(captureBtn, LinearLayout.LayoutParams(0, dp(52), 2f))
        camPanel.addView(prompt)
        camPanel.addView(row, LinearLayout.LayoutParams(MATCH_PARENT, WRAP_CONTENT).apply { topMargin = dp(8) })
        camLayer.addView(camPanel, FrameLayout.LayoutParams(MATCH_PARENT, WRAP_CONTENT, Gravity.BOTTOM))
        content.addView(camLayer, FrameLayout.LayoutParams(MATCH_PARENT, MATCH_PARENT))

        // connect panel
        connectPanel = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setBackgroundColor(dark)
            setPadding(dp(24), dp(40), dp(24), dp(24))
        }
        val title = TextView(this).apply {
            text = "Connect to Cube Robot Studio"; setTextColor(Color.WHITE); textSize = 22f
            typeface = Typeface.DEFAULT_BOLD
        }
        val help = TextView(this).apply {
            setTextColor(Color.parseColor("#a39e96")); textSize = 14f
            text = "• Wi-Fi: phone and laptop on the same network – found automatically.\n" +
                "• USB (OTG cable/USB debugging): the laptop app runs adb reverse – found automatically.\n" +
                "• Bluetooth: turn on Bluetooth tethering (PAN) between phone and laptop, then enter " +
                "the laptop's address below.\n\nLaptop address (shown in the laptop app):"
        }
        hostEdit = EditText(this).apply {
            setText(prefs.getString("host", "") ?: "")
            hint = "192.168.1.20"
            setTextColor(Color.WHITE); setHintTextColor(Color.GRAY)
            inputType = InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_VARIATION_URI
        }
        val go = bigButton("Connect", "#3a7bf0") { link.connectTo(hostEdit.text.toString()) }
        val auto = bigButton("Search again", "#33312e") { link.autoConnect(prefs.getString("host", null)) }
        connectPanel.addView(title)
        connectPanel.addView(help, LinearLayout.LayoutParams(MATCH_PARENT, WRAP_CONTENT).apply { topMargin = dp(12) })
        connectPanel.addView(hostEdit)
        connectPanel.addView(go, LinearLayout.LayoutParams(MATCH_PARENT, dp(52)).apply { topMargin = dp(12) })
        connectPanel.addView(auto, LinearLayout.LayoutParams(MATCH_PARENT, dp(52)).apply { topMargin = dp(8) })
        content.addView(connectPanel, FrameLayout.LayoutParams(MATCH_PARENT, MATCH_PARENT))

        setContentView(root)

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

    private fun dp(v: Int) = (v * resources.displayMetrics.density).toInt()

    private fun smallButton(label: String, onClick: () -> Unit) = Button(this).apply {
        text = label; isAllCaps = false; textSize = 13f
        setTextColor(Color.WHITE); setBackgroundColor(Color.parseColor("#33312e"))
        setOnClickListener { onClick() }
        minWidth = dp(64); minimumWidth = dp(64)
    }

    private fun bigButton(label: String, color: String, onClick: () -> Unit) = Button(this).apply {
        text = label; isAllCaps = false; textSize = 16f; typeface = Typeface.DEFAULT_BOLD
        setTextColor(Color.WHITE); setBackgroundColor(Color.parseColor(color))
        setOnClickListener { onClick() }
    }

    private fun setStatus(s: String) = ui.post { status.text = s }

    private fun showConnectPanel(show: Boolean) = ui.post {
        connectPanel.visibility = if (show) View.VISIBLE else View.GONE
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
            if (overlay.width > 0) {
                val s = FaceSampler.sample(upright, previewView.width, previewView.height,
                    { u, v -> overlay.at(u, v) }, false)
                ui.post {
                    overlay.liveColors = s.preview
                    overlay.logoSeen = s.logoDetected
                    overlay.invalidate()
                }
            }
        } catch (_: Exception) {
        } finally {
            proxy.close()
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
        autoCaptureRunnable?.let { ui.removeCallbacks(it) }
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
                .put("index", promptIndex)
            link.post("/api/scan/capture", body) { res ->
                ui.post {
                    captureBtn.isEnabled = true
                    if (res != null && res.optBoolean("ok", false)) {
                        prompt.text = "Captured ✓  (robot is turning the cube…)"
                    } else {
                        waitingCapture = true
                        prompt.text = "Capture failed: ${res?.optString("error") ?: "no connection"} – tap Capture"
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
                val url = "http://$host:${Link.PORT}/phone"
                if (web.url != url) web.loadUrl(url)
            }
        } else if (host == null) {
            showConnectPanel(true)
        }
    }

    override fun onEvent(ev: JSONObject) {
        when (ev.optString("type")) {
            "scan_prompt" -> ui.post {
                promptIndex = ev.optInt("index")
                val name = ev.optString("color_name").uppercase()
                val total = ev.optInt("total", 6)
                waitingCapture = true
                showCamera(true)
                captureBtn.isEnabled = true
                val hint = ev.optString("manual_hint", "")
                prompt.text = "Scan $name face  (${promptIndex + 1}/$total)" +
                    if (hint.isNotEmpty() && hint != "null") "\n$hint" else ""
                autoCaptureRunnable?.let { ui.removeCallbacks(it) }
                if (ev.optBoolean("auto", true)) {
                    val r = Runnable { if (waitingCapture) captureNow() }
                    autoCaptureRunnable = r
                    ui.postDelayed(r, ev.optLong("settle_ms", 500) + 250)
                }
            }
            "scan_done" -> ui.post {
                waitingCapture = false
                prompt.text = "Scan complete ✓"
                ui.postDelayed({ if (!waitingCapture) showCamera(userCamera) }, 900)
            }
            "state" -> {
                val scanning = ev.optString("busy_task") == "scan"
                if (!scanning && waitingCapture) ui.post {
                    waitingCapture = false
                    showCamera(userCamera)
                }
            }
        }
    }
}
