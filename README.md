# Cube Robot Studio

Laptop app (**CubeRobotStudio.exe**) and phone app (**CubeRobotScanner.apk**) for the
[Yonni123/RubiksCubeRobot](https://github.com/Yonni123/RubiksCubeRobot) 4-arm robot.
The robot runs the **stock firmware unchanged**. The laptop talks to it over the Nano's USB serial
port (9600 baud) using only `PING`, `STATUS` and `SEQ`.

```
 phone (camera, 3D view) ──Wi-Fi / USB / BT tethering──►  laptop app  ──USB serial──►  Arduino Nano
                         ◄──────── live events ──────────  solver, 3D, face net
```

## Download

Get both files from the latest release on the **Releases** tab. Every push to `main` builds them
(`.github/workflows/build.yml`), and publishing a release `vX.Y` attaches them to it.

* **CubeRobotStudio.exe**: Windows 10/11, a single file. It opens in its own window using Edge WebView2,
  which is built into Windows. Allow it through the firewall the first time (TCP 8765, UDP 47800).
* **CubeRobotScanner.apk**: Android 7+. Install it over any older version.

## Connecting

* **Robot:** in the laptop app, pick the Arduino's COM port and click **Connect**. Ports are listed with
  their Windows names (as in Device Manager); the Arduino / CH340 one is marked ★ and pre-selected, and
  the list refreshes by itself. If the Arduino drops off USB, the app explains why and reconnects. *SIMULATOR* tries
  everything without hardware. After connecting, the log shows *Arduino answered* and the four slider
  positions. If there is no reply: check the COM port, unplug the HC-05/06 from RX/TX, close the Arduino
  IDE Serial Monitor, and make sure the firmware was flashed with `CALIBRATE false`.
* **Phone:** the laptop's **Pair a phone** panel shows a QR code and a 6-digit code (*New code* makes
  another one). In the phone app, tap **Scan QR code** or type the code. The phone finds the laptop over
  USB (`adb reverse`, set up automatically if `adb` is installed), the laptop's network announcement, or a
  search of its own Wi-Fi subnet. Typing the laptop's IP address also works.

## Power

The 8 servos need the external **5 V, 3 A or more** supply described in the robot's README, sharing
GND with the Arduino. The laptop's USB port gives only 0.5–0.9 A: the servos pull the voltage down, and
the Arduino resets or vanishes from USB ("file not found", COM port coming and going). Until an external
supply is connected, tick **Low-power mode** in the Robot panel. Sliders then hold at C (just touching)
instead of pressing at L, and the arms are not kept powered while waiting during scans.

## Robot commands

Slider states: **L = clamped (pressing)**, **R = released**. Spinners: L −90°, l −45°, C 0°, r +45°, R +90°.

* **Open:** all sliders to R.
* **Close:** front and back sliders to L, a **500 ms** pause, then right and left to L (`SEQ FLBL500RLLL`).
* **Solve / Scramble:** the firmware's `MOVE` command always leaves the sliders at C, so the app sends the
  same servo steps itself as `SEQ` commands, with every hold at **L**. A command never exceeds 250
  characters (firmware buffer: 256). After a U/D move the cube is flipped, and at the end the robot flips
  it back, so it ends where it started (chosen front and top).
* The firmware cuts power to the servos 3 s after a command. While the cube is lifted for the camera, the
  app re-sends the slider positions every 2 s so the arms keep holding it.

## Scanning

1. Clamp the cube in the robot **any way round**. Nothing is assumed about the centre colours.
2. Press **Scan** on the laptop or the phone. Nothing moves until you press **Start scanning**.
3. For each face, all four arms clamp. The arm holding the face **twists its layer 45°**, lets go and turns
   its gripper back to **vertical (C)**. The holding pair then rolls the cube 90°, so the face comes up on
   top with its layer twisted 45°, pointing straight at the robot front. The holders end **horizontal
   (R/L)** and the released arms stay **vertical (C)**. After Confirm the cube rolls back, the same arm
   re-grips the twisted layer and untwists it.
4. Aim the phone and press **📷 Scan face**, on the phone or on the laptop (the laptop tells the phone to
   capture). Then press **Confirm ✓** or **↺ Retake**. Keyboard: Enter = next step, R = retake.
5. The first two faces are shown by position ("left-arm face", "bottom face"). After the second face the
   app works out the cube's orientation from the two measured centres (two adjacent centres fix it). The
   other faces then show their colours. At the end all six centres are checked again.
   Example for red front / white top: green, yellow, blue, white (front/back arms lift), then red, orange
   (left/right arms lift). The cube ends where it started, every gripper vertical and clamped.

The **GAN logo** on the white centre is looked for on the phone and again on the laptop (from the face
picture). Both sample a disc, so the logo's direction does not matter, including on the 45° twisted face.
A centre with the logo always counts as white, never blue. The colours are classified from all 54
stickers together, with 9 per colour. If the result is not a valid cube, the app tries every quarter-turn of each face picture.
The servo strings can be edited and jogged in **Scan sequence tuning** (saved in
`~/.cuberobot/scan_plan.json`).

## Checking the robot commands

`tools/check_robot_commands.py` simulates the robot physically: stickers, layer turns, cube rolls, and
clamping only square to a face or a twisted layer. For scanning it also checks that the holders are
horizontal and the released arms vertical, and that the face on top is twisted 45°.

```
cd desktop-go && cp -r ../desktop/web web && DUMP_SEQ=/tmp/seqs.json go test -run TestDumpSolveSeqs .
python ../tools/check_robot_commands.py solve /tmp/seqs.json
```

`desktop-go` also has `go test -race ./...`: a full app flow against the built-in simulator, covering
scan, retake, confirm, solve, abort and close.

## Arduino wiring used here

`arduino/Config.h` has the D2–D9 pin map: front spinner/slider D2/D3, back D4/D5, right D6/D7,
left D8/D9. Recalibrate after rewiring. In calibration mode the servo numbers follow type, not pin.

## Layout

```
desktop-go/   laptop app (Go, no dependencies apart from WebView2 on Windows)
desktop/web/  laptop + phone UI (HTML/JS: face net, 3D cube, QR code)
desktop/      original Python prototype (solver/scan tests only; not shipped)
android/      phone app (Kotlin, CameraX, ZXing QR scanner)
tools/        robot command checkers
```
