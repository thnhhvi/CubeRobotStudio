package com.cuberobot.studio

import android.content.Context
import android.net.wifi.WifiManager
import org.json.JSONObject
import java.io.BufferedReader
import java.io.InputStreamReader
import java.net.DatagramPacket
import java.net.DatagramSocket
import java.net.HttpURLConnection
import java.net.InetSocketAddress
import java.net.SocketTimeoutException
import java.net.URL
import java.util.concurrent.Executors
import java.util.concurrent.atomic.AtomicBoolean

/**
 * Link to the laptop app (Cube Robot Studio).
 *  - USB:  laptop runs `adb reverse tcp:8765 tcp:8765` -> phone uses 127.0.0.1
 *  - Wi-Fi: laptop broadcasts a UDP beacon on port 47800 -> phone finds its IP
 *  - Bluetooth: use Bluetooth tethering (PAN); the laptop then has an IP like Wi-Fi
 * Events arrive as Server-Sent-Events on /api/events.
 */
class Link(private val ctx: Context) {
    companion object {
        const val PORT = 8765
        const val DISCOVERY_PORT = 47800
    }

    interface Listener {
        fun onStatus(connected: Boolean, host: String?, message: String)
        fun onEvent(ev: JSONObject)
    }

    var listener: Listener? = null
    @Volatile var host: String? = null
        private set
    private val io = Executors.newCachedThreadPool()
    private val running = AtomicBoolean(false)
    @Volatile private var generation = 0
    private var sseConn: HttpURLConnection? = null

    val baseUrl: String? get() = host?.let { "http://$it:$PORT" }

    /** Try saved host, USB (127.0.0.1), then UDP discovery. */
    fun autoConnect(saved: String?) {
        io.execute {
            listener?.onStatus(false, null, "Searching for laptop…")
            val candidates = mutableListOf<String>()
            if (!saved.isNullOrBlank()) candidates.add(saved)
            candidates.add("127.0.0.1")
            for (h in candidates) {
                if (probe(h)) { start(h); return@execute }
            }
            val found = discover(4000)
            if (found != null && probe(found)) { start(found); return@execute }
            listener?.onStatus(false, null,
                "Laptop not found. Same Wi-Fi? USB with adb? Enter the address shown in the laptop app.")
        }
    }

    fun connectTo(h: String) {
        io.execute {
            val clean = h.trim().removePrefix("http://").substringBefore(":").substringBefore("/")
            if (probe(clean)) start(clean)
            else listener?.onStatus(false, null, "No Cube Robot Studio at $clean:$PORT")
        }
    }

    private fun probe(h: String): Boolean = try {
        val c = URL("http://$h:$PORT/api/info").openConnection() as HttpURLConnection
        c.connectTimeout = 1200
        c.readTimeout = 1500
        val ok = c.responseCode == 200 && c.inputStream.bufferedReader().readText().contains("CubeRobotStudio")
        c.disconnect()
        ok
    } catch (e: Exception) {
        false
    }

    private fun discover(timeoutMs: Int): String? {
        val wifi = ctx.applicationContext.getSystemService(Context.WIFI_SERVICE) as WifiManager
        val lock = wifi.createMulticastLock("cuberobot")
        lock.setReferenceCounted(false)
        lock.acquire()
        try {
            DatagramSocket(null).use { s ->
                s.reuseAddress = true
                s.broadcast = true
                s.bind(InetSocketAddress(DISCOVERY_PORT))
                s.soTimeout = timeoutMs
                val buf = ByteArray(2048)
                val pkt = DatagramPacket(buf, buf.size)
                s.receive(pkt)
                val msg = String(pkt.data, 0, pkt.length)
                if (msg.contains("CubeRobotStudio")) return pkt.address.hostAddress
            }
        } catch (e: SocketTimeoutException) {
            return null
        } catch (e: Exception) {
            return null
        } finally {
            lock.release()
        }
        return null
    }

    private fun start(h: String) {
        stop()
        host = h
        running.set(true)
        val gen = ++generation
        listener?.onStatus(true, h, "Connected to $h")
        io.execute { sseLoop(h, gen) }
        io.execute { helloLoop(gen) }
    }

    fun stop() {
        running.set(false)
        try { sseConn?.disconnect() } catch (_: Exception) {}
        sseConn = null
    }

    private fun sseLoop(h: String, gen: Int) {
        while (running.get() && gen == generation) {
            try {
                val c = URL("http://$h:$PORT/api/events").openConnection() as HttpURLConnection
                c.connectTimeout = 3000
                c.readTimeout = 30000
                c.setRequestProperty("Accept", "text/event-stream")
                sseConn = c
                val r = BufferedReader(InputStreamReader(c.inputStream, Charsets.UTF_8))
                listener?.onStatus(true, h, "Connected to $h")
                while (running.get() && gen == generation) {
                    val line = r.readLine() ?: break
                    if (line.startsWith("data: ")) {
                        try { listener?.onEvent(JSONObject(line.substring(6))) } catch (_: Exception) {}
                    }
                }
            } catch (e: Exception) {
                if (running.get()) listener?.onStatus(false, h, "Reconnecting to $h…")
            }
            try { Thread.sleep(1500) } catch (_: InterruptedException) {}
        }
    }

    private fun helloLoop(gen: Int) {
        val id = android.provider.Settings.Secure.getString(ctx.contentResolver,
            android.provider.Settings.Secure.ANDROID_ID) ?: "phone"
        while (running.get() && gen == generation) {
            post("/api/phone/hello", JSONObject()
                .put("id", id)
                .put("name", android.os.Build.MODEL)
                .put("link", if (host == "127.0.0.1") "USB" else "Wi-Fi"))
            try { Thread.sleep(5000) } catch (_: InterruptedException) {}
        }
    }

    /** Fire-and-forget POST; callback (optional) gets the JSON reply on a worker thread. */
    fun post(path: String, body: JSONObject, cb: ((JSONObject?) -> Unit)? = null) {
        val base = baseUrl ?: run { cb?.invoke(null); return }
        io.execute {
            var res: JSONObject? = null
            try {
                val c = URL(base + path).openConnection() as HttpURLConnection
                c.requestMethod = "POST"
                c.connectTimeout = 3000
                c.readTimeout = 10000
                c.doOutput = true
                c.setRequestProperty("Content-Type", "application/json")
                c.outputStream.use { it.write(body.toString().toByteArray(Charsets.UTF_8)) }
                val stream = if (c.responseCode < 400) c.inputStream else c.errorStream
                res = JSONObject(stream.bufferedReader().readText())
                c.disconnect()
            } catch (_: Exception) {
            }
            cb?.invoke(res)
        }
    }
}
