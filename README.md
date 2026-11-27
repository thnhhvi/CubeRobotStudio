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

Slider states: **H = clamped a little harder than L** (only while the cube is flipped or rolled; set by
*Flip grip* in the Robot panel, needs the Cube Robot Studio firmware), **L = clamped (pressing)**,
**C = just touching**, **R = released**. Spinners: L −90°, l −45°, C 0°, r +45°, R +90°.

Safety rules for the scan commands the app builds itself (solves run the firmware's own MOVE):

* **One type at a time.** A step moves sliders or spinners, never both, and the next step only starts
  when this one is done: at least **400 ms** after sliders, **300 ms** after one spinner and **400 ms**
  after two spinners rolling the cube (or the speed setting, if slower).
* **Clamp before release.** One step never clamps one slider while releasing another: the clamp finishes
  first, then the other arm lets go.
* **Always held.** After every step at least one opposite pair (front + back or left + right) holds the cube.
* **Hard grip while rolling (scan).** The pair carrying the cube during a scan roll clamps at H (Flip
  grip, default 40 µs past L), the other pair lets go, and once the roll is done the other pair clamps and
  the carriers ease back to L.

* **Open:** right and left sliders to R, a **500 ms** pause, then front and back (`SEQ RRLR500FRBR`).
* **Close:** front and back sliders to L, a **500 ms** pause, then right and left to L (`SEQ FLBL500RLLL`).
* **Solve / Scramble:** the firmware's `MOVE` command (see *Solving* below). Scan steps are `SEQ` commands
  (never more than 250 characters; firmware buffer 256).

## Solving (as in the original CubeSolver app)

The solver is the one the original Yonni123 CubeSolver app uses: Kociemba's two-phase algorithm, first
solution of at most 21 moves (1 s limit). The robot performs it with the firmware's own `MOVE` command,
exactly like the original app: the firmware turns F/B with the front/back arms, flips the cube (front/back
arms) to bring U/D to the left/right arms and back for L/R, and holds the cube with the sliders at C.
The app mirrors the firmware's timing for the 3D view. A `MOVE` holds at most 63 moves; longer move lists
are split. With *Return home* on, a cube left flipped is flipped back at the end.

## Scanning

1. Clamp the cube in the robot **any way round**. Nothing is assumed about the centre colours.
2. Press **Scan** on the laptop or the phone. Nothing moves until you press **Start scanning**.
3. The face already on top is scanned first, without a roll. Each further face is rolled straight up by
   one pair (pre-turn its grippers while released, clamp hard, the other pair lets go, roll). In the scan
   pose the rolling pair is **clamped at L with its grippers vertical** and the other pair **touches at C**.
   No 45° twist. Example for red front / white top: white, green, yellow, blue
   (front/back roll), red, orange (left/right roll), then one last roll brings the cube back to where it
   started.
4. Aim the phone and press **📷 Scan face**, on the phone or on the laptop (the laptop tells the phone to
   capture). Then press **Confirm ✓** or **↺ Retake**. Keyboard: Enter = next step, R = retake.
5. The first two faces are shown by position ("left-arm face", "bottom face"). After the second face the
   app works out the cube's orientation from the two measured centres (two adjacent centres fix it). The
   other faces then show their colours. At the end all six centres are checked again.
   The cube ends where it started, every gripper vertical and clamped.

**Black grippers** are treated as an object in front of the cube: their pixels are ignored when a
sticker is read (on the phone and again on the laptop from the face picture), and a sticker that is mostly
covered is marked hidden (an X on the phone, ? in the net). When a face has hidden stickers the robot takes
a **second look**: the other pair clamps, the rolling pair lets go, the phone captures again, and the cube
is handed back. Anything still hidden at the end is worked out from the rest of the cube (every piece exists
once, 9 stickers per colour, twist/flip/parity); only a unique answer is used, otherwise the ? stickers are
left for you to tap.

The **GAN logo** on the white centre is looked for on the phone and again on the laptop (from the face
picture). Both sample a disc, so the logo's direction does not matter.
A centre with the logo always counts as white, never blue. The colours are classified from all 54
stickers together, with 9 per colour. If the result is not a valid cube, the app tries every quarter-turn of each face picture.
The servo strings can be edited and jogged in **Scan sequence tuning** (saved in
`~/.cuberobot/scan_plan.json`).

## Checking the robot commands

`tools/check_robot_commands.py` simulates the robot physically: stickers, layer turns, cube rolls, and
clamping only square to a face. It enforces the safety rules above (no slider+spinner step, no clamp and
release in one step, minimum step times, an opposite pair always holding). For scanning it also checks the scan
pose (one pair clamped at L, the other touching at C, every gripper vertical) and the second-look hand-off. Solves are checked physically with the firmware's own MOVE servo strings.

```
cd desktop-go && cp -r ../desktop/web web && DUMP_SEQ=/tmp/seqs.json go test -run TestDumpSolveSeqs .
python ../tools/check_robot_commands.py solve /tmp/seqs.json
```

`desktop-go` also has `go test -race ./...`: a full app flow against the built-in simulator, covering
scan, retake, confirm, solve, abort and close.

## Arduino wiring used here

`arduino/Arduino/Config.h` (firmware, see `arduino/README.md`) has the D2–D9 pin map: front spinner/slider D2/D3, back D4/D5, right D6/D7,
left D8/D9. Recalibrate after rewiring. In calibration mode the servo numbers follow type, not pin.

## Layout

```
desktop-go/   laptop app (Go, no dependencies apart from WebView2 on Windows)
desktop/web/  laptop + phone UI (HTML/JS: face net, 3D cube, QR code)
desktop/      original Python prototype (solver/scan tests only; not shipped)
android/      phone app (Kotlin, CameraX, ZXing QR scanner)
tools/        robot command checkers
```
