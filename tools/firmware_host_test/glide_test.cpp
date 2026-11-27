// Host test of the firmware's synced spinner glide (no Arduino needed):
//   g++ -std=gnu++17 -I tools/firmware_host_test -I arduino/Arduino tools/firmware_host_test/glide_test.cpp \
//       arduino/Arduino/MyServo.cpp arduino/Arduino/SequenceManager.cpp -o /tmp/glide && /tmp/glide
#include "Arduino.h"
#include "MyServo.h"
#include "SequenceManager.h"
#include <cmath>
#include <cstdio>

SerialT Serial;
static unsigned long NOW = 0;
unsigned long millis() { return NOW; }
void pinMode(int, int) {}
// two spinners with different calibrations, like real servos
ServoCal readCalibration(ServoType t) {
    return t == BACK_SPINNER ? ServoCal{550, 2500, 1550, 30} : ServoCal{550, 2500, 1600, 30};
}

static double progress(int p, int from, int to) { return (double)(p - from) / (double)(to - from); }

int main() {
    attachAllServos();
    seqManager.glide = true;
    // pre-turn, then roll back to true centre, then a single face turn (no glide)
    seqManager.startSequence("flbr300fLbR300flbr300fCfCbCbC300rR300");
    double worst = 0;
    int fFrom = 1600, bFrom = 1550, fTo = 550, bTo = 2500;
    for (NOW = 0; NOW <= 1500; NOW++) {
        updateAllServos();
        seqManager.tick();
        int f = servos[FRONT_SPINNER].pulseWidth(), b = servos[BACK_SPINNER].pulseWidth();
        if (NOW > 100 && NOW < 700) // both travel C -> L / C -> R: same progress at every instant
            worst = fmax(worst, fabs(progress(f, fFrom, fTo) - progress(b, bFrom, bTo)));
        if (NOW == 400 && (f == fFrom || f == fTo)) { printf("FAIL: no glide (f=%d at 400 ms)\n", f); return 1; }
    }
    if (worst > 0.03) { printf("FAIL: spinners out of sync by %.0f%%\n", worst * 100); return 1; }
    if (servos[FRONT_SPINNER].pulseWidth() != 1600 || servos[BACK_SPINNER].pulseWidth() != 1550) {
        printf("FAIL: not back at true centre\n"); return 1;
    }
    if (servos[RIGHT_SPINNER].pulseWidth() != 2500) { printf("FAIL: single spinner move\n"); return 1; }
    printf("firmware glide: paired spinners stay within %.1f%% of each other\n", worst * 100);
    return 0;
}
