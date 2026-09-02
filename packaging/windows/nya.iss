; NYA Windows installer (Inno Setup 6)
; Built by .github/workflows/release.yml
;
; Defines (passed by ISCC):
;   MyAppVersion  e.g. 0.1.0
;   StagingDir    folder containing nya.exe, nya-get.exe

#ifndef MyAppVersion
  #define MyAppVersion "0.0.0-dev"
#endif
#ifndef StagingDir
  #define StagingDir "..\..\dist\windows-amd64"
#endif

#define MyAppName "NYA"
#define MyAppPublisher "Nyarime"
#define MyAppURL "https://github.com/nyarime/nya"
#define MyAppExeName "nya.exe"

[Setup]
AppId={{A7C3E9F1-4B2D-4E8A-9C1F-8D2E3F4A5B6C}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppPublisher={#MyAppPublisher}
AppPublisherURL={#MyAppURL}
AppSupportURL={#MyAppURL}
AppUpdatesURL={#MyAppURL}
DefaultDirName={localappdata}\Programs\{#MyAppName}
DefaultGroupName={#MyAppName}
DisableProgramGroupPage=yes
LicenseFile=..\..\LICENSE
OutputDir=..\..\dist
OutputBaseFilename=nya-{#MyAppVersion}-windows-amd64-setup
SetupIconFile=
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
ArchitecturesInstallIn64BitMode=x64compatible
ArchitecturesAllowed=x64compatible
ChangesEnvironment=yes
PrivilegesRequired=lowest
PrivilegesRequiredOverridesAllowed=dialog

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"; Flags: unchecked
Name: "associate"; Description: "Associate .nya → nya open; .nyam → nya get"; GroupDescription: "File associations:"; Flags: checkedonce
Name: "addpath"; Description: "Add install directory to user PATH"; GroupDescription: "Environment:"; Flags: checkedonce

[Files]
Source: "{#StagingDir}\nya.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#StagingDir}\nya-get.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\README.md"; DestDir: "{app}"; Flags: ignoreversion isreadme
Source: "..\..\LICENSE"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{group}\NYA CLI help"; Filename: "{app}\{#MyAppExeName}"; Parameters: "help"
Name: "{group}\{cm:UninstallProgram,{#MyAppName}}"; Filename: "{uninstallexe}"
Name: "{autodesktop}\NYA"; Filename: "{app}\{#MyAppExeName}"; Parameters: "help"; Tasks: desktopicon

[Registry]
Root: HKCU; Subkey: "Environment"; ValueType: expandsz; ValueName: "Path"; \
    ValueData: "{olddata};{app}"; Tasks: addpath; Check: NeedsAddPath(ExpandConstant('{app}'))

Root: HKCU; Subkey: "Software\Classes\.nya"; ValueType: string; ValueName: ""; ValueData: "Nyarime.NYA"; Flags: uninsdeletevalue; Tasks: associate
Root: HKCU; Subkey: "Software\Classes\Nyarime.NYA"; ValueType: string; ValueName: ""; ValueData: "NYA Archive"; Flags: uninsdeletekey; Tasks: associate
Root: HKCU; Subkey: "Software\Classes\Nyarime.NYA\DefaultIcon"; ValueType: string; ValueName: ""; ValueData: "{app}\{#MyAppExeName},0"; Tasks: associate
Root: HKCU; Subkey: "Software\Classes\Nyarime.NYA\shell\open\command"; ValueType: string; ValueName: ""; \
    ValueData: """{app}\{#MyAppExeName}"" open ""%1"""; Tasks: associate

Root: HKCU; Subkey: "Software\Classes\.nyam"; ValueType: string; ValueName: ""; ValueData: "Nyarime.NYAM"; Flags: uninsdeletevalue; Tasks: associate
Root: HKCU; Subkey: "Software\Classes\Nyarime.NYAM"; ValueType: string; ValueName: ""; ValueData: "NYA Download Manifest"; Flags: uninsdeletekey; Tasks: associate
Root: HKCU; Subkey: "Software\Classes\Nyarime.NYAM\shell\open\command"; ValueType: string; ValueName: ""; \
    ValueData: """{app}\{#MyAppExeName}"" get ""%1"""; Tasks: associate

[Run]
Filename: "{app}\{#MyAppExeName}"; Parameters: "help"; Description: "{cm:LaunchProgram,NYA}"; Flags: nowait postinstall skipifsilent

[Code]
function NeedsAddPath(Param: string): boolean;
var
  OrigPath: string;
begin
  if not RegQueryStringValue(HKEY_CURRENT_USER, 'Environment', 'Path', OrigPath)
  then begin
    Result := True;
    exit;
  end;
  Result := Pos(';' + Param + ';', ';' + OrigPath + ';') = 0;
end;
