#include "calibrate.h"
#include "MyServo.h"
#include "SequenceManager.h"
#include "API.h"

void setup() {
    Serial.begin(9600);
    Serial.setTimeout(3000);
    calibrateSetup();   // Initialize EEPROM where calibrations are stored
#if !CALIBRATE
    APISetup();        // Setup API if not in calibration mode
#endif
}

void loop() {
    // During calibration, seqManager is never busy  
#if KEEP_ATTACHED
    attachAllServos();  // Cube Robot Studio: servos always powered, like in calibration mode
#else
    if (CALIBRATE)
        attachAllServos();
    else if (millis() - seqManager.idleTimeMs < 3000 || seqManager.isBusy())
        attachServosStaggered();   // Cube Robot Studio: one at a time (power surge)
    else
        detachAllServos();
#endif
        
#if CALIBRATE
    calibrateLoop();
#else
    APILoop();  // Listen for API commands
    int res = seqManager.tick();    // If ongoing sequence, keep going
    if (res < 0)
    {
        Serial.print("SEQ ERR ");
        Serial.println(res);
    }
#endif
}
