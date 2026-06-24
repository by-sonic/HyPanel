@echo off
setlocal enabledelayedexpansion

REM HyPanel Windows Control Script
REM This script provides a menu-driven interface for managing HyPanel on Windows

cd /d "%~dp0"
set "SERVICE_NAME=hypanel"
set "INSTALL_DIR=%HYPANEL_HOME%"
if "%INSTALL_DIR%"=="" set "INSTALL_DIR=C:\Program Files\hypanel"

:menu
cls
echo ========================================
echo HyPanel Windows Control Panel
echo ========================================
echo.
echo Current directory: %INSTALL_DIR%
echo.
echo 1. Start HyPanel Service
echo 2. Stop HyPanel Service
echo 3. Restart HyPanel Service
echo 4. Check Service Status
echo 5. View Service Logs
echo 6. Open Panel in Browser
echo 7. Run HyPanel Manually
echo 8. Install/Uninstall Service
echo 9. Open Installation Directory
echo 10. Show Configuration
echo 11. Show Access URLs
echo 0. Exit
echo.
echo ========================================

set /p choice="Please select an option [0-11]: "

if "%choice%"=="1" goto start_service
if "%choice%"=="2" goto stop_service
if "%choice%"=="3" goto restart_service
if "%choice%"=="4" goto check_status
if "%choice%"=="5" goto view_logs
if "%choice%"=="6" goto open_panel
if "%choice%"=="7" goto run_manual
if "%choice%"=="8" goto service_management
if "%choice%"=="9" goto open_directory
if "%choice%"=="10" goto show_config
if "%choice%"=="11" goto show_urls
if "%choice%"=="0" goto exit
goto invalid_choice

:start_service
echo Starting HyPanel service...
net start %SERVICE_NAME%
if %errorLevel% equ 0 (
    echo Service started successfully!
) else (
    echo Failed to start service. Error code: %errorLevel%
)
pause
goto menu

:stop_service
echo Stopping HyPanel service...
net stop %SERVICE_NAME%
if %errorLevel% equ 0 (
    echo Service stopped successfully!
) else (
    echo Failed to stop service. Error code: %errorLevel%
)
pause
goto menu

:restart_service
echo Restarting HyPanel service...
net stop %SERVICE_NAME% >nul 2>&1
timeout /t 2 /nobreak >nul
net start %SERVICE_NAME%
if %errorLevel% equ 0 (
    echo Service restarted successfully!
) else (
    echo Failed to restart service. Error code: %errorLevel%
)
pause
goto menu

:check_status
echo Checking HyPanel service status...
sc query %SERVICE_NAME%
echo.
echo Service status details:
for /f "tokens=3 delims=: " %%i in ('sc query %SERVICE_NAME% ^| find "STATE"') do (
    echo Current state: %%i
)
pause
goto menu

:view_logs
echo Opening HyPanel logs...
if exist "%INSTALL_DIR%\logs" (
    start "" "%INSTALL_DIR%\logs"
) else (
    echo Logs directory not found: %INSTALL_DIR%\logs
)
pause
goto menu

:open_panel
echo Opening HyPanel panel in browser...
start http://localhost:2095
echo Panel opened in default browser.
pause
goto menu

:run_manual
echo Running HyPanel manually...
if exist "%INSTALL_DIR%\hypanel.exe" (
    cd /d "%INSTALL_DIR%"
    echo Starting HyPanel in current window...
    echo Press Ctrl+C to stop
    echo.
    hypanel.exe
) else (
    echo HyPanel executable not found: %INSTALL_DIR%\hypanel.exe
    echo Please run the installer first.
)
pause
goto menu

:service_management
cls
echo ========================================
echo Service Management
echo ========================================
echo.
echo 1. Install Windows Service
echo 2. Uninstall Windows Service
echo 3. Back to Main Menu
echo.
set /p service_choice="Select option [1-3]: "

if "%service_choice%"=="1" goto install_service
if "%service_choice%"=="2" goto uninstall_service
if "%service_choice%"=="3" goto menu
goto invalid_choice

:install_service
echo Installing Windows Service...
if exist "%INSTALL_DIR%\hypanel-service.exe" (
    cd /d "%INSTALL_DIR%"
    hypanel-service.exe install
    if %errorLevel% equ 0 (
        echo Service installed successfully!
        echo Starting service...
        net start %SERVICE_NAME%
    ) else (
        echo Failed to install service. Error code: %errorLevel%
    )
) else (
    echo Service wrapper not found. Please run the installer first.
)
pause
goto service_management

:uninstall_service
echo Uninstalling Windows Service...
if exist "%INSTALL_DIR%\hypanel-service.exe" (
    cd /d "%INSTALL_DIR%"
    net stop %SERVICE_NAME% >nul 2>&1
    hypanel-service.exe uninstall
    if %errorLevel% equ 0 (
        echo Service uninstalled successfully!
    ) else (
        echo Failed to uninstall service. Error code: %errorLevel%
    )
) else (
    echo Service wrapper not found.
)
pause
goto service_management

:open_directory
echo Opening installation directory...
if exist "%INSTALL_DIR%" (
    start "" "%INSTALL_DIR%"
) else (
    echo Installation directory not found: %INSTALL_DIR%
)
pause
goto menu

:show_config
echo.
echo ========================================
echo HyPanel Configuration
echo ========================================
if exist "%INSTALL_DIR%\hypanel.exe" (
    cd /d "%INSTALL_DIR%"
    echo Current settings:
    hypanel.exe setting -show
    echo.
    echo Admin credentials:
    hypanel.exe admin -show
) else (
    echo HyPanel executable not found. Please run the installer first.
)
pause
goto menu

:show_urls
echo.
echo ========================================
echo Access URLs
echo ========================================
echo.
echo Local access:
echo   Panel: http://localhost:2095
echo   Subscription: http://localhost:2096
echo.
echo Network access:
for /f "tokens=2 delims=:" %%i in ('ipconfig ^| findstr /i "IPv4"') do (
    set "ip=%%i"
    set "ip=!ip: =!"
    echo   Panel: http://!ip!:2095
    echo   Subscription: http://!ip!:2096
)
echo.
pause
goto menu

:invalid_choice
echo Invalid choice. Please select a valid option.
pause
goto menu

:exit
echo Thank you for using HyPanel Windows Control Panel!
exit /b 0
