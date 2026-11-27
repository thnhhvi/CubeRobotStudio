"""Cube Robot Studio - laptop app entry point.

Starts the local server (robot serial link, phone link, solver) and opens the
UI in a native window (pywebview / Edge WebView2) or, if that is unavailable,
in the default browser.
"""
import argparse
import sys
import time
import webbrowser

from cuberobot.controller import Controller
from cuberobot.server import PORT, local_ips, serve


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--browser", action="store_true", help="open in the default browser")
    ap.add_argument("--headless", action="store_true", help="server only")
    ap.add_argument("--port", type=int, default=PORT)
    args = ap.parse_args()

    ctl = Controller()
    httpd, stop = serve(ctl, port=args.port)
    url = f"http://127.0.0.1:{args.port}/"
    print(f"Cube Robot Studio running at {url}")
    for ip in local_ips():
        print(f"  phone (Wi-Fi):  http://{ip}:{args.port}")

    if args.headless:
        try:
            while True:
                time.sleep(1)
        except KeyboardInterrupt:
            pass
        return

    if not args.browser:
        try:
            import webview  # pywebview
            webview.create_window("Cube Robot Studio", url, width=1440, height=900,
                                  min_size=(1100, 720), background_color="#1f1e1d")
            webview.start()
            stop.set()
            return
        except Exception as e:  # fall back to browser
            print("Native window unavailable:", e)
    webbrowser.open(url)
    try:
        while True:
            time.sleep(1)
    except KeyboardInterrupt:
        stop.set()


if __name__ == "__main__":
    sys.exit(main())
