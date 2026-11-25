[Setup]
AppId=OnRDP
AppName=OnRDP
AppVersion={#ReleaseVersion}
AppPublisher=OnRDP
AppPublisherURL=https://github.com/diiyw/onrdp
DefaultDirName={localappdata}\Programs\OnRDP
DisableProgramGroupPage=yes
PrivilegesRequired=lowest
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
OutputBaseFilename=onrdp-windows-amd64-setup
UninstallDisplayIcon={app}\onrdp.exe

[Files]
Source: "{#BundleDir}\*"; DestDir: "{app}"; Flags: ignoreversion recursesubdirs createallsubdirs

[Icons]
Name: "{autoprograms}\OnRDP"; Filename: "{app}\onrdp.exe"; WorkingDir: "{app}"
