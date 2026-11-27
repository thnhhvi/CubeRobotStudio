#include <Arduino.h>
#include <math.h>
#include <Servo.h>
#include "MyServo.h"
#include "Config.h"
#include "calibrate.h"
#include "SequenceManager.h"

#define MIN_PULSE_WIDTH 250
#define MAX_PULSE_WIDTH 3000

MyServo::MyServo(int pin, ServoType type, ServoCal calibration) :
    pin(pin),
    type(type),
    cal(calibration)
{
    if (static_cast<int>(type) % 2 == 0) {
        pulse = calibration.C_us;  // We want spinners default at center
    } else {
        pulse = calibration.R_us;  // We want sliders in release by default
    }
}

void MyServo::setState(ServoState next, unsigned int glide)
{
    if (next == STATE_T)
    {
        // slider only: start at C (touching) and follow the roll in update()
        tracking = (type % 2 == 1) && seqManager.rollMs > 0;
        glideMs = 0;
        state = STATE_C;
        if (!tracking || !attached)
        {
            tracking = false;
            pulse = cal.C_us;
            if (attached)
                thisServo.writeMicroseconds(pulse);
        }
        return;
    }
    tracking = false;
    int target = pulse;
    switch (next)
    {
    case STATE_L:
        target = cal.L_us; break;
    case STATE_H:
        if (type % 2 == 1) // Slider: press a little further than L (away from C)
        {
            int extra = seqManager.hardGripUs;
            target = (cal.L_us >= cal.C_us) ? (int)cal.L_us + extra : (int)cal.L_us - extra;
        }
        else
            target = cal.L_us;
        break;
    case STATE_R:
        target = cal.R_us; break;
    case STATE_C:
        if (type % 2 == 1) // Slider
        {
            target = cal.C_us;
            break;
        }  
        if (state == STATE_L || state == STATE_l)
            target = cal.C_us + cal.CD_us;
        else if (state == STATE_R || state == STATE_r)
            target = cal.C_us - cal.CD_us;
        else
            target = cal.C_us;   // Double center is absolute center
        break;
    case STATE_r:
        target = (cal.C_us + cal.R_us) / 2; break;
    case STATE_l:
        target = (cal.C_us + cal.L_us) / 2; break;
    }
    state = next;
    if (glide > 0 && attached)
    {
        // a second command in the same instant ("fCfC") just retargets the glide
        if (glideMs == 0 || millis() != glideStart)
        {
            fromPulse = pulse;
            glideStart = millis();
        }
        toPulse = target;
        glideMs = glide;
        return;
    }
    glideMs = 0;
    pulse = target;
    thisServo.writeMicroseconds(pulse);
}

void MyServo::update(unsigned long now)
{
    if (tracking)
    {
        // roll angle follows the spinners' smoothstep; the cube's half-width along this
        // slider is (cos a + sin a) / 2 of the edge: back off by (cos a + sin a - 1)
        unsigned long t = now - seqManager.rollStart;
        int p;
        if (seqManager.rollMs == 0 || t >= seqManager.rollMs)
        {
            p = cal.C_us;
            tracking = false;
        }
        else
        {
            float x = (float)t / (float)seqManager.rollMs;
            float a = x * x * (3.0f - 2.0f * x) * 1.5707963f;
            float off = (cosf(a) + sinf(a) - 1.0f) / 0.41421356f;   // 0 .. 1 (at 45 deg)
            p = (int)cal.C_us + (int)(((int)cal.R_us - (int)cal.C_us) * off * seqManager.trackPct / 100.0f);
        }
        if (p != pulse)
        {
            pulse = p;
            if (attached)
                thisServo.writeMicroseconds(pulse);
        }
        return;
    }
    if (glideMs == 0)
        return;
    unsigned long t = now - glideStart;
    if (t >= glideMs)
    {
        pulse = toPulse;
        glideMs = 0;
    }
    else
    {
        // smoothstep: gentle start and stop, so the servo can follow the path
        float x = (float)t / (float)glideMs;
        float e = x * x * (3.0f - 2.0f * x);
        int p = fromPulse + (int)((toPulse - fromPulse) * e);
        if (p == pulse)
            return;
        pulse = p;
    }
    if (attached)
        thisServo.writeMicroseconds(pulse);
}

void MyServo::attach()
{
    if (!attached)
    {
        thisServo.attach(pin, MIN_PULSE_WIDTH, MAX_PULSE_WIDTH);
        thisServo.writeMicroseconds(pulse);
        attached = true;
    }
}

void MyServo::detach()
{
    if (attached)
    {
        thisServo.detach();
        attached = false;
    }
}

MyServo servos[NUM_SERVOS] =
{
    [RIGHT_SPINNER] = MyServo(RIGHT_SPINNER_PIN, RIGHT_SPINNER, readCalibration(RIGHT_SPINNER)),
    [RIGHT_SLIDER]  = MyServo(RIGHT_SLIDER_PIN,  RIGHT_SLIDER,  readCalibration(RIGHT_SLIDER)),
    [LEFT_SPINNER]  = MyServo(LEFT_SPINNER_PIN,  LEFT_SPINNER,  readCalibration(LEFT_SPINNER)),
    [LEFT_SLIDER]   = MyServo(LEFT_SLIDER_PIN,   LEFT_SLIDER,   readCalibration(LEFT_SLIDER)),
    [FRONT_SPINNER] = MyServo(FRONT_SPINNER_PIN, FRONT_SPINNER, readCalibration(FRONT_SPINNER)),
    [FRONT_SLIDER]  = MyServo(FRONT_SLIDER_PIN,  FRONT_SLIDER,  readCalibration(FRONT_SLIDER)),
    [BACK_SPINNER]  = MyServo(BACK_SPINNER_PIN,  BACK_SPINNER,  readCalibration(BACK_SPINNER)),
    [BACK_SLIDER]   = MyServo(BACK_SLIDER_PIN,   BACK_SLIDER,   readCalibration(BACK_SLIDER))
};

bool parseServoType(char c, ServoType& type)
{
    switch (c)
    {
        case 'R': type = RIGHT_SLIDER; return true;
        case 'L': type = LEFT_SLIDER; return true;
        case 'F': type = FRONT_SLIDER; return true;
        case 'B': type = BACK_SLIDER; return true;
        case 'r': type = RIGHT_SPINNER; return true;
        case 'l': type = LEFT_SPINNER; return true;
        case 'f': type = FRONT_SPINNER; return true;
        case 'b': type = BACK_SPINNER; return true;
        default: return false;
    }
}

void servoTypeToString(ServoType type, char* buffer, size_t bufferSize)
{
    switch (type)
    {
    case FRONT_SLIDER:
        strncpy(buffer, "FRONT_SLIDER", bufferSize); break;
    case FRONT_SPINNER:
        strncpy(buffer, "FRONT_SPINNER", bufferSize); break;
    case RIGHT_SLIDER:
        strncpy(buffer, "RIGHT_SLIDER", bufferSize); break;
    case RIGHT_SPINNER:
        strncpy(buffer, "RIGHT_SPINNER", bufferSize); break;
    case BACK_SLIDER:
        strncpy(buffer, "BACK_SLIDER", bufferSize); break;
    case BACK_SPINNER:
        strncpy(buffer, "BACK_SPINNER", bufferSize); break;
    case LEFT_SLIDER:
        strncpy(buffer, "LEFT_SLIDER", bufferSize); break;
    case LEFT_SPINNER:
        strncpy(buffer, "LEFT_SPINNER", bufferSize); break;
    default:
        strncpy(buffer, "UNKNOWN", bufferSize); break;
    }
}

void attachAllServos()
{
    for (int i = 0; i < NUM_SERVOS; i++)
    {
        servos[i].attach();
    }
}

void updateAllServos()
{
    unsigned long now = millis();
    for (int i = 0; i < NUM_SERVOS; i++)
    {
        servos[i].update(now);
    }
}

void detachAllServos()
{
    for (int i = 0; i < NUM_SERVOS; i++)
    {
        servos[i].detach();
    }
}

// ====================
// Calibration adjustment
// ====================

void MyServo::adjustCalibration(int delta)
{
    if (state == STATE_C)
    {
        cal.C_us += delta;
    }
    else if (state == STATE_L)
    {
        cal.L_us += delta;
    }
    else if (state == STATE_R)
    {
        cal.R_us += delta;
    }
    pulse += delta;
    thisServo.writeMicroseconds(pulse);
}

void MyServo::adjustDeviation(int delta)
{
    if (type % 2 == 0) // Spinner
    {
        // Check if CD was subtracted or added previously
        if (pulse < cal.C_us)
        {
            cal.CD_us -= delta;
            if (cal.CD_us < 0)
                cal.CD_us = 0;
            pulse = cal.C_us - cal.CD_us;
        }
        else if (pulse > cal.C_us)
        {
            cal.CD_us += delta;
            pulse = cal.C_us + cal.CD_us;
        }
        thisServo.writeMicroseconds(pulse);
    }
}
