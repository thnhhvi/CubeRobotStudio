#pragma once
#include <stdint.h>
#include <stddef.h>
#include <string.h>
#include <stdlib.h>
#include <ctype.h>
#include <stdio.h>
unsigned long millis();
void pinMode(int,int);
#define OUTPUT 1
struct SerialT { void begin(long){} void setTimeout(long){} int available(){return 0;} int read(){return 0;}
 size_t readBytesUntil(char,char*,size_t){return 0;}
 template<class T> void print(T){} template<class T> void println(T){} void println(){} template<class T> void print(T,int){} template<class T> void println(T,int){} };
extern SerialT Serial;
