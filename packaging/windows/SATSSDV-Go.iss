#define MyAppName "SATSSDV-Go"
#define MyAppVersion "0.16.0"
#define MyAppPublisher "BH2VSQ"
#ifndef SourceDir
  #define SourceDir "..\..\build-qt6\bin"
#endif

[Setup]
AppId={{A9E5224D-AB61-4A5A-9B25-2572E835A911}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppVerName={#MyAppName} {#MyAppVersion}
AppPublisher={#MyAppPublisher}
DefaultDirName={autopf}\{#MyAppName}
DefaultGroupName={#MyAppName}
DisableProgramGroupPage=yes
PrivilegesRequired=admin
ArchitecturesAllowed=x64
ArchitecturesInstallIn64BitMode=x64
OutputDir=..\..\dist\windows
OutputBaseFilename=SATSSDV-Go-Setup-{#MyAppVersion}
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
UninstallDisplayName={#MyAppName}
CloseApplications=yes
RestartApplications=no

[Tasks]
Name: "desktopicon"; Description: "Create a desktop shortcut"; GroupDescription: "Additional shortcuts:"; Flags: unchecked

[Files]
Source: "{#SourceDir}\*"; DestDir: "{app}"; Flags: ignoreversion recursesubdirs createallsubdirs

[Icons]
Name: "{autoprograms}\{#MyAppName}"; Filename: "{app}\SATSSDV-Go.exe"; WorkingDir: "{app}"
Name: "{autodesktop}\{#MyAppName}"; Filename: "{app}\SATSSDV-Go.exe"; WorkingDir: "{app}"; Tasks: desktopicon

[Run]
Filename: "{app}\SATSSDV-Go.exe"; Description: "Launch {#MyAppName}"; Flags: nowait postinstall skipifsilent
