#pragma once

enum ServoState
{
    STATE_C,
    STATE_L,
    STATE_R,
    STATE_r,    // 45 degree between CENTER and RIGHT
    STATE_l,  // 45 degree between CENTER and LEFT
    STATE_H,  // sliders: clamp a little harder than L (HARD_GRIP_US further), used while flipping
    STATE_T   // sliders: follow the cube while the other pair rolls it (touching at 0 and 90 deg,
              // backing off for the corners in between); ends at C
};

enum ServoType
{   // If a servo type is odd, it's a slider; if even, it's a spinner
    RIGHT_SPINNER,
    RIGHT_SLIDER,
    LEFT_SPINNER,
    LEFT_SLIDER,
    FRONT_SPINNER,
    FRONT_SLIDER,
    BACK_SPINNER,
    BACK_SLIDER
};

struct ServoCal {
    unsigned int L_us;
    unsigned int R_us;
    unsigned int C_us;
    unsigned int CD_us; // Deviation for center position for spinners
};

enum CubeOrientation
{
    ORIENT_NORMAL,
    ORIENT_INVERT
};
