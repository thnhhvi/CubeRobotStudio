// Replays every SEQ string the app sends through the real firmware code (host build, with
// this robot's calibration) and checks: accepted, no SEQ errors, each command finishes,
// and the servos wake one at a time.
//   DUMP_FW=/tmp/fw.txt go test -run TestDumpFirmwareSeqs .   (in desktop-go)
//   g++ -std=gnu++17 -I tools/firmware_host_test -I arduino/Arduino \
//       tools/firmware_host_test/replay_test.cpp arduino/Arduino/MyServo.cpp \
//       arduino/Arduino/SequenceManager.cpp -o /tmp/replay && /tmp/replay /tmp/fw.txt
#include "Arduino.h"
#include "MyServo.h"
#include "SequenceManager.h"
#include <cstdio>
#include <cstring>
#include <string>

SerialT Serial;
static unsigned long NOW = 0;
unsigned long millis() { return NOW; }
void pinMode(int, int) {}

// this robot's calibration (L, R, C, CD) in servo type order
static const ServoCal CAL[8] = {
    {520, 2450, 1520, 0},  {2505, 1350, 2250, 0}, // right spinner, right slider
    {600, 2550, 1650, 0},  {2550, 1100, 1755, 0}, // left
    {550, 2500, 1600, 0},  {450, 1800, 1100, 0},  // front
    {550, 2500, 1550, 0},  {450, 1750, 1000, 0},  // back
};
ServoCal readCalibration(ServoType t) { return CAL[t]; }

static void run(unsigned long ms) {
    for (unsigned long i = 0; i < ms; i++) { NOW++; seqManager.tick(); }
}

int main(int argc, char** argv) {
    FILE* f = fopen(argc > 1 ? argv[1] : "/tmp/fw.txt", "r");
    if (!f) { printf("no input\n"); return 2; }
    attachAllServos();
    char line[1024];
    int n = 0, bad = 0;
    while (fgets(line, sizeof line, f)) {
        line[strcspn(line, "\r\n")] = 0;
        if (!*line) continue;
        n++;
        int started;
        if (strncmp(line, "MOVE ", 5) == 0) {   // MOVE <delay> <orientation> <moves>
            int d = 0, o = 0;
            char mv[256] = {0};
            sscanf(line + 5, "%d %d %255s", &d, &o, mv);
            seqManager.orientation = o ? ORIENT_INVERT : ORIENT_NORMAL;
            started = seqManager.startMoves(mv, d);
        } else {
            if (strlen(line) > 250) { printf("too long (%zu): %s\n", strlen(line), line); bad++; continue; }
            started = seqManager.startSequence(line);
        }
        if (started != 0) { printf("rejected: %s\n", line); bad++; continue; }
        unsigned long limit = NOW + 120000;
        while (seqManager.isBusy() && NOW < limit) {
            NOW++;
            int r = seqManager.tick();
            if (r < 0) { printf("SEQ ERR %d: %s\n", r, line); bad++; break; }
        }
        if (seqManager.isBusy()) { printf("never finished: %s\n", line); bad++; }
        run(200);
    }
    // waking up: servos are powered one at a time, all of them before a SEQ starts (100 ms)
    detachAllServos();
    int maxAt = 0, last = 0;
    for (int t = 0; t < 100; t++) {
        NOW++;
        attachServosStaggered();
        int on = 0;
        for (int i = 0; i < NUM_SERVOS; i++) on += servos[i].isAttached();
        if (on - last > maxAt) maxAt = on - last;
        last = on;
    }
    if (last != NUM_SERVOS || maxAt > 1) { printf("staggered wake-up wrong (%d attached, %d at once)\n", last, maxAt); bad++; }
    printf("firmware replay: %d app commands, %d problems\n", n, bad);
    return bad ? 1 : 0;
}
