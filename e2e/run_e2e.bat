@echo off
rem Run ALL joyWaApi e2e tests against a real JoyWatcher + PLC (includes the JoyWSrv2 restart test).
rem Prompts for the PLC address and the tag list path. They can also be given as arguments:
rem   run_e2e.bat [PLC address] [tag list xlsx]
rem NOTE: writes to the PLC (restored at the end) and stops/restarts JoyWSrv2 for ~10 seconds.
rem Keep this file ASCII only: multibyte characters break cmd.exe parsing depending on code page.
setlocal
cd /d %~dp0

set "PLC=%~1"
set "TAGLIST=%~2"

if not defined PLC set /p "PLC=PLC address (e.g. 192.168.0.10 or 192.168.0.10:5000): "
if not defined PLC (
  echo PLC address is required.
  goto :fail
)
rem Default port is 5000
echo %PLC%| find ":" >nul || set "PLC=%PLC%:5000"

if not defined TAGLIST set /p "TAGLIST=Tag list (xlsx) path: "
if not defined TAGLIST (
  echo Tag list path is required.
  goto :fail
)
rem Strip surrounding quotes
set "TAGLIST=%TAGLIST:"=%"
if not exist "%TAGLIST%" (
  echo Tag list not found: %TAGLIST%
  goto :fail
)

echo.
echo PLC      : %PLC%
echo Tag list : %TAGLIST%
echo All e2e tests will run (about 3 minutes).
echo This writes to the PLC and restarts JoyWSrv2.
set /p "OK=Continue? (y/N): "
if /i not "%OK%"=="y" (
  echo Aborted.
  goto :end
)

set "JOYWA_E2E=1"
set "JOYWA_E2E_PLC=%PLC%"
set "JOYWA_E2E_TAGLIST=%TAGLIST%"
set "JOYWA_E2E_RESTART=1"
set "GOARCH=386"

go test -tags server -count=1 -timeout 30m -v .
set "RC=%ERRORLEVEL%"
echo.
if "%RC%"=="0" (echo e2e: PASSED) else (echo e2e: FAILED [exit code %RC%])
pause
exit /b %RC%

:fail
set "RC=1"
:end
pause
exit /b %RC%
