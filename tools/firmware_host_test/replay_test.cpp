// Replays every SEQ string the app sends through the real firmware code (host build, with
// this robot's calibration) and checks: accepted, no SEQ errors, paired spinners and paired
// sliders glide in sync, following sliders (T) really follow the roll and end at C, hard
// grip (H) is beyond L, and every command ends with the robot idle.
//   DUMP_FW=/tmp/fw.txt go test -run TestDumpFirmwareSeqs .   (in desktop-go)
//   g++ -std=gnu++17 -I tools/firmware_host_test -I arduino/Arduino \
//       tools/firmware_host_test/replay_test.cpp arduino/Arduino/MyServo.cpp \
//       arduino/Arduino/SequenceManager.cpp -o /tmp/replay && /tmp/replay /tmp/fw.txt
#include "Arduino.h"
#include "MyServo.h"
#include "SequenceManager.h"
#include <cmath>
#include <cstdio>
#include <cstring>
#include <string>

SerialT Serial;
static unsigned long NOW = 0;
unsigned long millis() { return NOW; }
void pinMode(int, int) {}

// this robot's calibration (L, R, C, CD) in servo type order
static const ServoCal CAL[8] = {
    {520, 2450, 1520, 0},  {2505, 1250, 2250, 0}, // right spinner, right slider
    {600, 2550, 1650, 0},  {2050, 950, 1755, 0},  // left
    {550, 2500, 1600, 0},  {650, 1850, 1100, 0},  // front
    {550, 2500, 1550, 0},  {550, 1500, 800, 0},   // back
};
ServoCal readCalibration(ServoType t) { return CAL[t]; }

int main(int argc, char** argv) {
    FILE* f = fopen(argc > 1 ? argv[1] : "/tmp/fw.txt", "r");
    if (!f) { printf("no input\n"); return 2; }
    attachAllServos();
    seqManager.glide = true;
    char line[1024];
    int n = 0, bad = 0;
    const ServoType sliders[4] = {FRONT_SLIDER, BACK_SLIDER, RIGHT_SLIDER, LEFT_SLIDER};
    const char sname[4] = {'F', 'B', 'R', 'L'};
    while (fgets(line, sizeof line, f)) {
        line[strcspn(line, "\r\n")] = 0;
        if (!*line) continue;
        n++;
        std::string seq(line);
        if (seq.size() > 250) { printf("too long (%zu): %s\n", seq.size(), line); bad++; continue; }
        if (seqManager.startSequence(line) != 0) { printf("rejected: %s\n", line); bad++; continue; }
        int maxDev[4] = {0, 0, 0, 0};
        bool follow[4];
        for (int k = 0; k < 4; k++) {
            char pat[3] = {sname[k], 'T', 0};
            follow[k] = seq.find(pat) != std::string::npos;
        }
        unsigned long start = NOW, limit = NOW + 60000;
        double worstSync = 0;
        while (seqManager.isBusy() && NOW < limit) {
            NOW++;
            updateAllServos();
            int r = seqManager.tick();
            if (r < 0) { printf("SEQ ERR %d: %s\n", r, line); bad++; break; }
            for (int k = 0; k < 4; k++)
                if (follow[k]) {
                    int d = abs(servos[sliders[k]].pulseWidth() - (int)CAL[sliders[k]].C_us);
                    if (servos[sliders[k]].getState() == STATE_C && d > maxDev[k]) maxDev[k] = d;
                }
            // pairs gliding together must be at the same point of their path
            const ServoType pairs[4][2] = {{FRONT_SPINNER, BACK_SPINNER}, {RIGHT_SPINNER, LEFT_SPINNER},
                                           {FRONT_SLIDER, BACK_SLIDER}, {RIGHT_SLIDER, LEFT_SLIDER}};
            for (auto& pr : pairs)
                if (servos[pr[0]].gliding() && servos[pr[1]].gliding())
                    worstSync = fmax(worstSync, fabs(servos[pr[0]].glideProgress() - servos[pr[1]].glideProgress()));
        }
        if (seqManager.isBusy()) { printf("never finished: %s\n", line); bad++; }
        for (int k = 0; k < 4; k++)
            if (follow[k]) {
                int want = abs((int)CAL[sliders[k]].R_us - (int)CAL[sliders[k]].C_us) * seqManager.trackPct / 100;
                if (maxDev[k] < want * 9 / 10 || maxDev[k] > want + 5) {
                    printf("%c does not follow the roll (max %d us, want %d): %s\n", sname[k], maxDev[k], want, line);
                    bad++;
                }
                if (servos[sliders[k]].pulseWidth() != (int)CAL[sliders[k]].C_us && seq.find(std::string(1, sname[k]) + "T") == seq.size() - 2) {
                    printf("%c not back at C\n", sname[k]); bad++;
                }
            }
        if (worstSync > 0.02) { printf("spinner pair out of sync (%.0f%%): %s\n", worstSync * 100, line); bad++; }
        for (int i = 0; i < 400; i++) { NOW++; updateAllServos(); seqManager.tick(); }
        (void)start;
    }
    // hard grip goes beyond L, away from C
    seqManager.startSequence("FHRH600");
    for (int i = 0; i < 800; i++) { NOW++; updateAllServos(); seqManager.tick(); }
    if (servos[FRONT_SLIDER].pulseWidth() != 650 - 40 || servos[RIGHT_SLIDER].pulseWidth() != 2505 + 40) {
        printf("hard grip wrong\n"); bad++;
    }
    printf("firmware replay: %d app commands, %d problems\n", n, bad);
    return bad ? 1 : 0;
}
