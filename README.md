# Cube Robot Studio

Laptop app (**CubeRobotStudio.exe**) and phone app (**CubeRobotScanner.apk**) for the
[Yonni123/RubiksCubeRobot](https://github.com/Yonni123/RubiksCubeRobot) 4-arm robot.
No firmware changes are needed: the laptop talks to the stock Arduino API (`PING`, `STATUS`,
`SEQ`, `MOVE`) over the Nano's USB serial port at 9600 baud.

```
 phone (camera) ──Wi-Fi / USB (adb reverse) / BT tethering──►  laptop app  ──USB serial──►  Arduino Nano
      ▲  live 3D model + buttons (WebView)                       │ solver, 3D, net editor
      └───────────────────────── Server-Sent Events ─────────────┘
```

## Ready-made laptop build

`CubeRobotStudio.exe` is a single native Windows program built from `desktop-go/` (Go, no dependencies).
Double-click it and the laptop UI opens in your browser. The console window lists the address the phone app
can connect to. The phone side is the Android APK in `android/` (build it as described below).

Rebuild the exe from any OS with Go 1.22+:
`cd desktop-go && cp -r ../desktop/web web && GOOS=windows GOARCH=amd64 go build -ldflags "-s -w" -o CubeRobotStudio.exe .`

## Building the installers (Python exe + native APK)

Push this folder to a GitHub repository. The workflow `.github/workflows/build.yml` builds both:

* **windows-exe**: cross-compiles the Go app in `desktop-go/` into `CubeRobotStudio.exe`, a single file.
* **core-tests**: runs the solver, scan-mapping and simulated-robot tests.
* **android-apk**: runs Gradle `assembleRelease`. The APK is signed with the debug key so you can sideload it.

Download both files from the run's **Artifacts**. If you push a tag such as `v1.0`, they are also attached to a GitHub Release.

To build locally instead:

* **exe**: run `desktop\build_exe.bat` on Windows with Python 3.10 or newer.
* **apk**: open `android/` in Android Studio and choose *Build → Build APK*, or run `gradlew assembleRelease`.
* **No build**: `python desktop/main.py` runs the laptop app straight from source (`pip install -r desktop/requirements.txt`).

## Connecting

| Link | Robot ⇄ laptop | Phone ⇄ laptop |
|---|---|---|
| USB | Arduino Nano USB cable (pick the COM port, or *SIMULATOR* to try without hardware) | Turn on USB debugging. If `adb` is on PATH or in the Android SDK, the app runs `adb reverse tcp:8765 tcp:8765` itself and the phone connects to 127.0.0.1 |
| Wi-Fi | – | Same network. The laptop sends a UDP beacon on port 47800 and the phone finds it automatically |
| Bluetooth | – | Turn on Bluetooth tethering (PAN) and type the laptop's PAN IP into the phone |

Allow `CubeRobotStudio.exe` through the Windows firewall (TCP 8765, UDP 47800) the first time.

## Laptop UI

The layout follows Cubzor's solver:

* **Face net**: a cross-shaped net laid out in the robot frame (U top, F front arm, R right arm, B back arm, L left arm, D bottom). It has a colour palette for fixing single stickers.
* **Front/top picker**: choose which colours sit in the front arm and on top. The net, the 3D model and the scan order update to match.
* **3D model**: drag to orbit, scroll to zoom, double-click to reset. The default view is from the robot front, the logo side. During a solve the model turns each layer in step with the robot, including the whole-cube flips the robot makes for U and D moves.
* **Move track**: the solution as move chips, with ⏮ ◀ ▶ ⏭ to preview it on the model before the robot runs it.
* **Robot panel**: **Open**, **Close**, **Scramble**, **Solve** and **Abort**, the same buttons as the original app. The speed slider sets the servo delay from **500 ms (slow) to 120 ms (fast)**.
* **Scan sequence tuning**: every servo string the scan uses can be edited and *jogged* one at a time to match your calibration. They are saved in `~/.cuberobot/scan_plan.json`.

The phone's 3D view has the same robot buttons and speed slider.

## Scan workflow

This example uses red in the front arm and white on top.

1. Press **Scan**, on the laptop or on the phone. The phone switches to its camera. The first time, drag the 4 blue corners onto the face at the spot where the robot presents it. The grid is a perspective quad, so a face tilted by 45° still samples correctly.
2. **Front/back-axis phase.** The front and back spinners roll the cube while the left and right arms release. Each roll ends with the left and right arms gripping again while the spinners re-centre.
   - **green** is rolled up and tilted 45° towards the robot front, then scanned. The same arms turn it back 45°.
   - **yellow**, then **blue**, get the same move. After blue, white is held by the LEFT arm.
   - **white** comes last. After it is turned back, white is on top again.
3. **Left/right-axis phase.** The left and right arms now hold green and blue.
   - **red** is tilted 45° towards the robot front and scanned.
   - The cube then flips so **orange** can be scanned.
4. The cube returns to red front, white top.

With auto-capture on, the phone captures each face once the robot reports IDLE plus a short settle time. You can also tap **Capture** yourself.

Colour classification happens on the laptop. It compares all 54 stickers together in Lab space, uses the actual centre colours under your lighting as references, and forces exactly 9 stickers per colour.

* **GAN logo**: during scanning, the white centre's blue GAN logo is detected on the phone (the camera grid shows "GAN ✓"), so the centre is not read as blue. The logo is not drawn on the 3D model or the net. The centre colours also come from your front/top choice. The app warns you if a logo shows up on a centre that should not be white.
* **Auto-orient**: if the result is not a valid cube, the app tries every quarter-turn rotation of each face image (4⁶ combinations) and keeps the valid one. This means the camera's in-plane rotation does not have to be exact.

## Solving

The laptop has its own Kociemba two-phase solver, written in pure Python with numpy tables bundled in the exe. It keeps searching for `solve_timeout` seconds (3 s by default) and collects shorter and shorter solutions.

It then picks the solution with the shortest **robot time**, not just the fewest moves. U and D moves force the robot to flip the cube, which costs 6 extra servo delays, so the move count alone is misleading.

When you press **Solve**, the laptop:

1. Sends `MOVE <delay> <orientation> <moves>` in chunks that fit the firmware's 64-byte buffer.
2. Tracks the cube's orientation.
3. Animates every move from the firmware's timing model and snaps to the final state when the robot reports `IDLE`.
4. If the cube ended flipped, sends the firmware's rotate-back sequence, so the cube finishes in its original pose with the chosen front and top.

## Notes / limits

* The robot can only roll the cube around its two horizontal arm axes. It cannot spin the cube around the vertical axis. "Facing the robot front" therefore means the face is rolled up and tilted 45° towards the front-right (first phase) or front-left (second phase). Mount the phone in front of the robot looking slightly down. If your camera position differs, fix the corners once on the phone and, if needed, set the per-step image rotation in *Scan sequence tuning*.
* The default scan servo strings are built from the same primitives the firmware uses (`rotateCube`, `{d}` = delay). Jog each step once at slow speed to check that it suits your calibration before running a full scan.
* Bluetooth uses PAN tethering (an IP link), not the HC-06 serial profile. The robot itself is driven over USB.

## Layout

```
desktop/   laptop app (Python): cuberobot/{twophase,cube,scan,robot,controller,server}.py, web/ UI, tests/
android/   phone app (Kotlin, CameraX, no Compose)
.github/workflows/build.yml   builds exe + apk
```
