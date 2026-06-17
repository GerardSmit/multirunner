@echo off
rem multirunner: runs at the end of Windows Setup (image is IMAGE_STATE_COMPLETE),
rem as SYSTEM, before the first interactive logon. Scans the attached CDs for
rem install-golden.ps1 and runs it to provision the runner + boot task.
for %%d in (D E F G H I J K L) do if exist %%d:\install-golden.ps1 powershell -NoProfile -ExecutionPolicy Bypass -File %%d:\install-golden.ps1
