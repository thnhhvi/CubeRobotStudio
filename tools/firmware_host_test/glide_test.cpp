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
    if (t == FRONT_SLIDER) return ServoCal{650, 1850, 1100, 0};   // L below C
    if (t == RIGHT_SLIDER) return ServoCal{2505, 1250, 2250, 0};  // L above C
    return t == BACK_SPINNER ? ServoCal{550, 2500, 1550, 30} : ServoCal{550, 2500, 1600, 30};
}

static double progress(int p, int from, int to) { return (double)(p - from) / (double)(to - from); }

int main() {
    attachAllServos();
    seqManager.glide = true;
    // pre-turn, roll back to true centre (one 90 deg glide each), a single face turn (no
    // glide), then the hard grip used while flipping
    seqManager.startSequence("fLbR600fCfCbCbC600rR300FHRH600");
    double worst = 0;
    int fFrom = 1600, bFrom = 1550, fTo = 550, bTo = 2500;
    int lastF = fFrom;
    bool slidersGlided = false;
    for (NOW = 0; NOW <= 2400; NOW++) {
        updateAllServos();
        seqManager.tick();
        int f = servos[FRONT_SPINNER].pulseWidth(), b = servos[BACK_SPINNER].pulseWidth();
        if (NOW > 100 && NOW < 700) { // both travel C -> L / C -> R: same progress at every instant
            worst = fmax(worst, fabs(progress(f, fFrom, fTo) - progress(b, bFrom, bTo)));
            if (f > lastF) { printf("FAIL: glide turns back (hesitates) at %lu ms\n", NOW); return 1; }
            lastF = f;
        }
        if (NOW == 1700) { // sliders F and R half way through their paired glide
            int F = servos[FRONT_SLIDER].pulseWidth(), R = servos[RIGHT_SLIDER].pulseWidth();
            double pf = progress(F, 1850, 650 - 40), pr = progress(R, 1250, 2505 + 40);
            slidersGlided = pf > 0.05 && pf < 0.95 && fabs(pf - pr) < 0.03;
        }
        if (NOW == 400 && (f == fFrom || f == fTo)) { printf("FAIL: no glide (f=%d at 400 ms)\n", f); return 1; }
        // glide ends at 70% of the delay: settled well before the next step (at 700 ms)
        if (NOW == 100 + 600 * 7 / 10 + 1 && f != fTo) { printf("FAIL: glide not done at 70%%\n"); return 1; }
    }
    if (worst > 0.03) { printf("FAIL: spinners out of sync by %.0f%%\n", worst * 100); return 1; }
    if (servos[FRONT_SPINNER].pulseWidth() != 1600 || servos[BACK_SPINNER].pulseWidth() != 1550) {
        printf("FAIL: not back at true centre\n"); return 1;
    }
    if (servos[RIGHT_SPINNER].pulseWidth() != 2500) { printf("FAIL: single spinner move\n"); return 1; }
    if (servos[FRONT_SLIDER].pulseWidth() != 650 - 40 || servos[RIGHT_SLIDER].pulseWidth() != 2505 + 40) {
        printf("FAIL: hard grip H (F=%d R=%d)\n", servos[FRONT_SLIDER].pulseWidth(), servos[RIGHT_SLIDER].pulseWidth());
        return 1;
    }
    if (!slidersGlided) { printf("FAIL: paired sliders do not glide together\n"); return 1; }
    printf("firmware glide: paired spinners stay within %.1f%% of each other\n", worst * 100);
    return 0;
}
