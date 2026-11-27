@echo off
REM Build CubeRobotStudio.exe on Windows (Python 3.10+ installed and on PATH)
cd /d %~dp0
python -m pip install -r requirements.txt pyinstaller || exit /b 1
python build_tables.py || exit /b 1
pyinstaller --noconfirm --onefile --windowed --name CubeRobotStudio ^
  --add-data "web;web" --add-data "cuberobot\twophase_v3.npz;cuberobot" ^
  --collect-submodules webview main.py || exit /b 1
echo.
echo Done: dist\CubeRobotStudio.exe
