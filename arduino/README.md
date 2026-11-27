# Firmware for Cube Robot Studio

`Arduino/` is the Yonni123/RubiksCubeRobot firmware with two changes:

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

2. **Synced spinners (`GLIDE` command).** With the stock firmware every servo jumps to its new position
   at full speed. When two spinners turn together to roll the cube, one of them is always a bit faster,
   and on a light speed cube that is enough to turn one face instead of the whole cube. With `GLIDE 1`
   (the app sends it on Connect), spinners commanded in the same step - and sliders commanded in the same
   step - move along one timed, smooth path
   over 70% of the delay that follows the step (the rest lets them settle), so they stay in sync. A single servo in a step
   moves as before.

3. **Hard grip (`H` state, `GRIP` command).** `H` clamps a slider `HARD_GRIP_US` (default 40 µs, about 4°)
   further than its L position. The app uses it for the pair carrying the cube while flipping it and sets
   the amount with `GRIP <us>` (Flip grip in the Robot panel, 0–150).

4. **Following sliders (`T` state, `FOLLOW` command).** In a roll step the non-carrying pair is sent `T`:
   it stays in contact with the rolling cube as a base, at C for 0° and 90° and backed off by `TRACK_PCT`
   (default 60) % of its C→R travel at 45°, where the corners stick out most. It follows the same timing
   as the rolling spinners and ends at C. `FOLLOW <pct>` sets the amount (Roll follow in the app).

The `SEQ`/`MOVE`/`STATUS`/`PING` API is otherwise unchanged.

## Upload

1. Download `CubeRobotFirmware.zip` from the release page and unzip it.
2. Open `Arduino/Arduino.ino` in the Arduino IDE, board **Arduino Nano** (processor *ATmega328P (Old
   Bootloader)* if the upload fails), select the COM port, **Upload**.
3. Close the Serial Monitor, then Connect in Cube Robot Studio. The log shows
   "Firmware glide on: spinners that turn together move in sync".

Your **calibration is kept**: it lives in the EEPROM, which an upload does not erase.

Calibration mode (`CALIBRATE true`) numbers the servos by type, **not** by pin:
0 right spinner, 1 right slider, 2 left spinner, 3 left slider, 4 front spinner, 5 front slider,
6 back spinner, 7 back slider.
