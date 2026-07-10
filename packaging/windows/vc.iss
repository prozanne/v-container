; Inno Setup script for the v-container per-user installer.
; Compiled by the release workflow as:
;   iscc /DMyAppVersion=1.2.3 /DStageDir=..\..\stage /DOutputDir=..\..\dist vc.iss
; StageDir must contain vc.exe and the pruned qemu\ tree (see get-qemu.ps1).
;
; Installs per-user (no admin, no UAC) to %LOCALAPPDATA%\Programs\v-container
; and adds that directory to the user PATH. The AppId GUID is what makes
; upgrades install over the previous version — never change it.

#ifndef MyAppVersion
  #define MyAppVersion "0.0.0"
#endif
#ifndef StageDir
  #define StageDir "..\..\stage"
#endif
#ifndef OutputDir
  #define OutputDir "..\..\dist"
#endif

[Setup]
AppId={{FEA6BB1F-D874-4C9E-AB29-87830F083114}
AppName=v-container
AppVersion={#MyAppVersion}
AppPublisher=prozanne
AppPublisherURL=https://github.com/prozanne/v-container
AppSupportURL=https://github.com/prozanne/v-container/issues
DefaultDirName={autopf}\v-container
PrivilegesRequired=lowest
DisableProgramGroupPage=yes
DisableWelcomePage=yes
OutputDir={#OutputDir}
OutputBaseFilename=vc-setup-{#MyAppVersion}
Compression=lzma2
SolidCompression=yes
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
ChangesEnvironment=yes
UninstallDisplayIcon={app}\vc.exe
InfoAfterFile=postinstall.txt
WizardStyle=modern

[Files]
Source: "{#StageDir}\vc.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#StageDir}\qemu\*"; DestDir: "{app}\qemu"; Flags: recursesubdirs ignoreversion

[Registry]
Root: HKCU; Subkey: "Environment"; ValueType: expandsz; ValueName: "Path"; ValueData: "{olddata};{app}"; Check: NeedsAddPath(ExpandConstant('{app}'))

[Code]
{ True when Dir is not yet a component of the user Path. }
function NeedsAddPath(Dir: string): Boolean;
var
  Path: string;
begin
  if not RegQueryStringValue(HKCU, 'Environment', 'Path', Path) then
  begin
    Result := True;
    exit;
  end;
  Result := Pos(';' + Uppercase(Dir) + ';', ';' + Uppercase(Path) + ';') = 0;
end;

{ Remove exactly the entry the installer added, wherever it sits in Path. }
procedure RemoveFromPath(Dir: string);
var
  Path: string;
  P: Integer;
begin
  if not RegQueryStringValue(HKCU, 'Environment', 'Path', Path) then
    exit;
  P := Pos(';' + Uppercase(Dir) + ';', ';' + Uppercase(Path) + ';');
  if P = 0 then
    exit;
  if P = 1 then
    { First entry: remove it and the separator that follows (if any). }
    Delete(Path, 1, Length(Dir) + 1)
  else
    { Later entry: remove the separator before it plus the entry. }
    Delete(Path, P - 1, Length(Dir) + 1);
  RegWriteExpandStringValue(HKCU, 'Environment', 'Path', Path);
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
begin
  if CurUninstallStep = usPostUninstall then
    RemoveFromPath(ExpandConstant('{app}'));
end;
