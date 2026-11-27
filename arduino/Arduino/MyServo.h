#pragma once
#include <Arduino.h>
#include "Config.h"
#include "Types.h"
#include <Servo.h>

// ToString for servo types
void servoTypeToString(ServoType type, char* buffer, size_t bufferSize);

// Convert character to ServoType (uppercase is slider, lowercase is spinner)
bool parseServoType(char c, ServoType& type);

class MyServo
{
private:
    int pin{};
    Servo thisServo{};
    ServoType type{};
    ServoCal cal{};
    ServoState state{STATE_C};
    int pulse{};    // Current pulse width in microseconds
    bool attached{false};

    // Glide (Cube Robot Studio): move to the target over glideMs instead of at full
    // servo speed, so two spinners commanded together follow the same timed path.
    int fromPulse{};
    int toPulse{};
    unsigned long glideStart{};
    unsigned int glideMs{0};
    bool tracking{false};   // T: follow the roll of the other pair

public:

    // Constructor
    MyServo(int pin, ServoType type, ServoCal calibration);

    // State control. glide > 0: reach the new state over glide ms (smooth, timed).
    void setState(ServoState next, unsigned int glide = 0);

    // Advance a glide; call often (from loop()).
    void update(unsigned long now);

    ServoState getState() const
    {
        return state;
    }

    int pulseWidth() const
    {
        return pulse;
    }

    int getPin() const
    {
        return pin;
    }

    ServoType getType() const
    {
        return type;
    }

    ServoCal getCalibration() const
    {
        return cal;
    }

    void adjustCalibration(int delta);

    void adjustDeviation(int delta);

    void attach();

    void detach();
};

#define NUM_SERVOS 8

extern MyServo servos[NUM_SERVOS];

void attachAllServos();

void detachAllServos();

void updateAllServos();
