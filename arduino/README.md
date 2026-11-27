# Firmware for Cube Robot Studio

`Arduino/` is the Yonni123/RubiksCubeRobot firmware with these changes:

1. **Pins** (`Config.h`) set to this wiring, `CALIBRATE false`:

   | Pin | Servo | `Config.h` define |
   |---|---|---|
   | D2 | front spinner | `FRONT_SPINNER_PIN 2` |
   | D3 | front slider | `FRONT_SLIDER_PIN 3` |
   | D4 | back spinner | `BACK_SPINNER_PIN 4` |
   | D5 | back slider | `BACK_SLIDER_PIN 5` |
   | D6 | right spinner | `RIGHT_SPINNER_PIN 6` |
   | D7 | right slider | `RIGHT_SLIDER_PIN 7` |
   | D8 | left spinner | `LEFT_SPINNER_PIN 8` |
   | D9 | left slider | `LEFT_SLIDER_PIN 9` |

2. **Hard grip (`H` state, `GRIP` command).** `H` clamps a slider `HARD_GRIP_US` (default 40 µs, about 4°)
   further than its L position. The app uses it for the pair carrying the cube while flipping it and sets
   the amount with `GRIP <us>` (Flip grip in the Robot panel, 0–150).

3. **`KEEP_ATTACHED`** (default `false`): like the stock firmware the servos go limp 3 s after a command,
   which keeps the power draw low. `true` keeps them powered all the time, as in calibration mode (draws
   a lot of current while holding).

4. **`INFO`** answers `OK CRS <version>`, so the app knows this firmware runs.

5. **Servos wake one at a time** (`ATTACH_STAGGER_MS`, 12 ms apart) when a command arrives after they went
   limp, instead of all eight in the same instant. Each servo jumps to its position when it is powered;
   eight at once can pull the 5 V down so far that some of them do not move.

Servo motion is the stock firmware's (each servo jumps to its new position at full speed).

On boot the firmware prints `Cube Robot Studio firmware 3.1 (INFO GRIP, H state)`; the app shows the
version in the robot chip. The `SEQ`/`MOVE`/`STATUS`/`PING` API is otherwise unchanged.

## Upload

1. Download `CubeRobotFirmware.zip` from the release page and unzip it.
2. Open `Arduino/Arduino.ino` in the Arduino IDE, board **Arduino Nano** (processor *ATmega328P (Old
   Bootloader)* if the upload fails), select the COM port, **Upload**.
3. Close the Serial Monitor, then Connect in Cube Robot Studio. The log shows
   "Cube Robot Studio firmware 3.1".

Your **calibration is kept**: it lives in the EEPROM, which an upload does not erase.

Calibration mode (`CALIBRATE true`) numbers the servos by type, **not** by pin:
0 right spinner, 1 right slider, 2 left spinner, 3 left slider, 4 front spinner, 5 front slider,
6 back spinner, 7 back slider.
