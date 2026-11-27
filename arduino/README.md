# Arduino pin map used with this project

`Config.h` here is the stock firmware config (Yonni123/RubiksCubeRobot) with the servo pins set to this wiring:

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

Copy it over `Arduino/Config.h` of the firmware. No other firmware file needs to change, and the laptop/phone
apps do not depend on the pins.

Calibration mode (`CALIBRATE true`) numbers the servos by type, **not** by pin:
0 right spinner, 1 right slider, 2 left spinner, 3 left slider, 4 front spinner, 5 front slider,
6 back spinner, 7 back slider. Re-run the calibration after rewiring (values are stored per servo type in EEPROM).
