// Set these to the Arduino pin numbers connected to each servo
// If you change these, it is recommended to recalibrate the servos
#define RIGHT_SLIDER_PIN 7    // D7  RSl
#define RIGHT_SPINNER_PIN 6   // D6  RSp
#define LEFT_SLIDER_PIN 9     // D9  LSl
#define LEFT_SPINNER_PIN 8    // D8  LSp
#define FRONT_SLIDER_PIN 3    // D3  front slide
#define FRONT_SPINNER_PIN 2   // D2  front spin
#define BACK_SLIDER_PIN 5     // D5  BSl
#define BACK_SPINNER_PIN 4    // D4  BSp

// If true, run the servo calibration tool instead of normal operation
// Set true only when you want to recalibrate servos
// After running in calibration mode, open serial monitor and type "h" for help
// It will let you control individual servos to calibrate them perfectly
#define CALIBRATE false

// Size of the buffer to hold active sequences in SequenceManager (SEQ command)
// A move (like U) can be a sequence like "rR220RR220rCrC220RC220rR2".
// Solving the cube, expect 24 moves to be safe, this quickly blows up to over 1kB ish.
// Please try not to exceed this size or increase it if your RAM allows it.
#define SEQUENCE_BUFFER_SIZE 256

// Size of the buffer to hold individual moves in MOVE command
// God's number is 20, but make it more to be safe.
// (Realistically, we will almost never find a perfect 20-move solution, unless its an easy scramble)
// But, based on real testing, 32 moves were exceeded, so make it larger.
#define MOVE_BUFFER_SIZE 64

// Cube Robot Studio: state "H" clamps a slider this many microseconds further than its L
// position (pressing a little harder while the cube is flipped). The app can change it at
// runtime with "GRIP <us>" (0..150).
#define HARD_GRIP_US 40

// Cube Robot Studio: state "T" (track) for the non-carrying sliders during a roll. They
// stay in contact with the rolling cube as a base: at 0 and 90 deg at C (touching), at
// 45 deg backed off by this percentage of the C->R distance (a cube's corners stick out
// sqrt(2) times as far). "FOLLOW <pct>" changes it at runtime (0..100).
#define TRACK_PCT 60
