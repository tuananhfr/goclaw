# Skill Drafts Layout

This folder keeps both editable source folders and upload-ready ZIP packages.

## Current Layout

```text
skill-drafts/
  shared/
    facebook-fanpage-content-guidelines/
    fanpage-content-creator-guidelines/
    fanpage-design-guidelines/
    fanpage-internet-research-guidelines/
    fanpage-lead-orchestration-guidelines/
    packages/

  pages/
    pizza-hips/
      skills/
        brand-pizza-hips-guidelines/
        pizza-hips-franchise-knowledge/
      brand-kits/
        pizza-hips/
          BRAND.md
          render-preset.json
          assets/fonts/SVN-Bango.otf
      packages/
```

## What Goes Where

- `shared/*`: reusable skills for many pages and teams.
- `pages/{page_slug}/skills/*`: page-specific skills.
- `pages/{page_slug}/brand-kits/*`: team/global workspace materials, such as fonts, brand rules, render presets, and image references.
- `packages/`: ZIP files ready to upload.

## Pizza Hip'S Uploads

Skill system uploads:

```text
skill-drafts/shared/packages/facebook-fanpage-content-guidelines.zip
skill-drafts/shared/packages/fanpage-content-creator-guidelines.zip
skill-drafts/shared/packages/fanpage-design-guidelines.zip
skill-drafts/shared/packages/fanpage-internet-research-guidelines.zip
skill-drafts/shared/packages/fanpage-lead-orchestration-guidelines.zip
skill-drafts/pages/pizza-hips/packages/brand-pizza-hips-guidelines.zip
skill-drafts/pages/pizza-hips/packages/pizza-hips-franchise-knowledge.zip
```

Team/global workspace upload:

```text
skill-drafts/pages/pizza-hips/packages/brand-kits.zip
```

## Rebuild ZIPs After Editing Folders

Shared skill example:

```powershell
tar -a -cf `
  skill-drafts\shared\packages\facebook-fanpage-content-guidelines.zip `
  -C skill-drafts\shared\facebook-fanpage-content-guidelines .
```

Pizza Hip'S skill example:

```powershell
tar -a -cf `
  skill-drafts\pages\pizza-hips\packages\brand-pizza-hips-guidelines.zip `
  -C skill-drafts\pages\pizza-hips\skills\brand-pizza-hips-guidelines .
```

Pizza Hip'S brand kit:

```powershell
tar -a -cf `
  skill-drafts\pages\pizza-hips\packages\brand-kits.zip `
  -C skill-drafts\pages\pizza-hips brand-kits
```

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

Backslash entries caused the earlier font/path bug.

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
