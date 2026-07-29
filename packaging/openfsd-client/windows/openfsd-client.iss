; Inno Setup 6 — openfsd Client Setup (Windows)
; Installs the plain Go binary under Program Files and creates Start Menu entries.
; User settings live in %AppData%\openfsd-client\ (created by the app; not removed on uninstall).

#define MyAppName "openfsd Client Setup"
#define MyAppPublisher "openfsd"
#define MyAppURL "https://github.com/renorris/openfsd"
#define MyAppExeName "openfsd-client.exe"

#ifndef MyAppVersion
  #define MyAppVersion "0.0.0-dev"
#endif
#ifndef MyAppVersionInfo
  #define MyAppVersionInfo "0.0.0.0"
#endif
#ifndef SourceBin
  #define SourceBin "..\..\..\dist\openfsd-client\bin\windows-amd64\openfsd-client.exe"
#endif
#ifndef OutputDir
  #define OutputDir "..\..\..\dist\openfsd-client"
#endif
[Setup]
AppId={{A7E2C4B1-8F3D-4A9E-B2C1-0D5E6F7A8B9C}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppVerName={#MyAppName} {#MyAppVersion}
AppPublisher={#MyAppPublisher}
AppPublisherURL={#MyAppURL}
AppSupportURL={#MyAppURL}
AppUpdatesURL={#MyAppURL}
DefaultDirName={autopf}\openfsd-client
DefaultGroupName={#MyAppName}
DisableProgramGroupPage=yes
LicenseFile=..\..\..\LICENSE
OutputDir={#OutputDir}
OutputBaseFilename=openfsd-client-{#MyAppVersion}-windows-amd64-setup
#ifdef SetupIcon
SetupIconFile={#SetupIcon}
#endif
Compression=lzma2/ultra64
SolidCompression=yes
WizardStyle=modern
PrivilegesRequired=admin
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
UninstallDisplayIcon={app}\{#MyAppExeName}
UninstallDisplayName={#MyAppName}
VersionInfoVersion={#MyAppVersionInfo}
VersionInfoCompany={#MyAppPublisher}
VersionInfoDescription={#MyAppName} installer
VersionInfoProductName={#MyAppName}
CloseApplications=yes
RestartApplications=no
; Do not touch %AppData%\openfsd-client on uninstall — user settings.

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"; Flags: unchecked

[Files]
Source: "{#SourceBin}"; DestDir: "{app}"; DestName: "{#MyAppExeName}"; Flags: ignoreversion

[Icons]
Name: "{group}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"
Name: "{group}\{cm:UninstallProgram,{#MyAppName}}"; Filename: "{uninstallexe}"
Name: "{autodesktop}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"; Tasks: desktopicon

[Run]
Filename: "{app}\{#MyAppExeName}"; Description: "{cm:LaunchProgram,{#StringChange(MyAppName, '&', '&&')}}"; Flags: nowait postinstall skipifsilent

[Code]
// Optional: open config dir note is not needed — app creates it on first run.
// Config path: %AppData%\openfsd-client\settings.json
