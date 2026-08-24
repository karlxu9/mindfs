@echo off
cd /d E:\claude-workspace\mindfs
echo [%date% %time%] restart begin >> restart-log.txt
mindfs-new.exe -stop >> restart-log.txt 2>&1
timeout /t 2 /nobreak > nul
move /y mindfs-new.exe mindfs.exe >> restart-log.txt 2>&1
start "" mindfs.exe
timeout /t 10 /nobreak > nul
echo|set /p=health: >> restart-log.txt
curl -s -m 3 http://127.0.0.1:7331/health >> restart-log.txt 2>&1
echo. >> restart-log.txt
echo [%date% %time%] restart done >> restart-log.txt
schtasks /delete /tn MindFSBugfixRestart /f > nul 2>&1
