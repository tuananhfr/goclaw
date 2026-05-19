# Skill Draft Packages

This folder now keeps only upload-ready ZIP packages plus this README.

Source/unpacked skill folders were intentionally removed to keep the workspace clean for multiple pages.

## Current Layout

```text
skill-drafts/
  shared/
    packages/
      facebook-fanpage-content-guidelines.zip
      fanpage-content-creator-guidelines.zip
      fanpage-design-guidelines.zip
      fanpage-internet-research-guidelines.zip
      fanpage-lead-orchestration-guidelines.zip

  pages/
    pizza-hips/
      packages/
        brand-pizza-hips-guidelines.zip
        pizza-hips-franchise-knowledge.zip
        brand-kits.zip
        SVN-Bango.zip
```

## Usage

Use `shared/packages` for reusable fanpage skills that apply across many pages.

Use `pages/{page_slug}/packages` for page-specific skills and brand kits.

For Pizza Hip'S:

- Upload skill ZIPs from `skill-drafts/pages/pizza-hips/packages/` into the skill system when needed.
- Upload `brand-kits.zip` into the team/global workspace when agents need shared brand materials such as fonts and render presets.

## Add A New Page

Create only a packages folder:

```powershell
New-Item -ItemType Directory -Force `
  -Path "skill-drafts\pages\new-page\packages"
```

Put that page's upload-ready ZIP files there.

## ZIP Path Rule

ZIP entries must use Unix-style forward slashes:

```text
SKILL.md
assets/fonts/SVN-Bango.otf
brand-kits/pizza-hips/BRAND.md
```

Do not package entries with Windows backslashes:

```text
assets\fonts\SVN-Bango.otf
brand-kits\pizza-hips\BRAND.md
```

Backslash entries caused the earlier font/path bug: the file existed, but the runtime looked for `assets/fonts/SVN-Bango.otf` while the ZIP contained a Windows-style name.

## Verify ZIP Entries

```powershell
$zipPath = "skill-drafts\pages\pizza-hips\packages\brand-kits.zip"

Add-Type -AssemblyName System.IO.Compression
Add-Type -AssemblyName System.IO.Compression.FileSystem

$zip = [System.IO.Compression.ZipFile]::OpenRead((Resolve-Path $zipPath).Path)
try {
  $zip.Entries | Select-Object FullName, Length | Format-Table -AutoSize

  $bad = @($zip.Entries | Where-Object { $_.FullName -like '*\*' })
  if ($bad.Count -eq 0) {
    "OK: no backslash entries"
  } else {
    "ERROR: ZIP contains backslash entries"
    $bad | Select-Object FullName
  }
} finally {
  $zip.Dispose()
}
```
