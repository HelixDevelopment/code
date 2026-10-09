# W2a-1 evidence — colibri vendored + built — 2026-09-05

Submodule: https://github.com/JustVugg/colibri pinned at 8f08dbd (tag v1.10.1, Apache-2.0; SHA resolved live from remote).

## gitlink

## binary
-rwxrwxr-x 1 milosvasic milosvasic 607976 Sep  5 18:53 dependencies/colibri/c/colibri
dependencies/colibri/c/colibri: ELF 64-bit LSB pie executable, x86-64, version 1 (SYSV), dynamically linked, interpreter /lib64/ld-linux-x86-64.so.2, BuildID[sha1]=16abdd6574d6d9ca71482e1eef801b9f46935f85, for GNU/Linux 3.2.0, not stripped

## colibri --help (engine binary; launcher is ./coli serve --model <dir>)
```
[OMP] hot-thread tuning: re-exec once (COLI_NO_OMP_TUNE=1 to skip)
[OMP] colibri: 8 physical-core threads instead of 16 logical CPUs; SMT can halve decode throughput on some CPUs (#718); set OMP_NUM_THREADS=<n> to override
colibri: this is the GLM-5.2 engine, and it was started without a model.
The engine is not the program you run directly -- the launcher is:

    ./coli chat  --model <model directory>    interactive chat
    ./coli serve --model <model directory>    OpenAI-compatible API
    ./coli web   --model <model directory>    API plus the dashboard
    ./coli doctor --model <model directory>   check a model is usable

The launcher needs Python 3 and picks the right engine for the model.
Getting a model, step by step: https://github.com/JustVugg/colibri/blob/main/docs/quickstart.md
```
