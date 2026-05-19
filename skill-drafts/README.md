# Skill ZIP Packaging Notes

This folder contains draft skills and their upload-ready ZIP files.

## Important Path Rule

Skill ZIP entries must use Unix-style forward slashes:

```text
SKILL.md
assets/fonts/SVN-Bango.otf
```

Do not create ZIP entries with Windows backslashes:

```text
assets\fonts\SVN-Bango.otf
```

That second form can become a single filename containing backslash characters in some extraction/runtime contexts. When that happens, a skill that references:

```text
{baseDir}/assets/fonts/SVN-Bango.otf
```

will fail because the actual extracted file is named:

```text
assets\fonts\SVN-Bango.otf
```

instead of being inside real `assets/fonts/` directories.

## Do Not Use PowerShell Compress-Archive For Skills

On this Windows setup, `Compress-Archive` created ZIP entries like:

```text
assets\fonts\SVN-Bango.otf
```

That was the root cause of the font lookup issue. The font existed, permissions were fine, but the ZIP entry path did not match the skill reference.

## Recommended Packaging Command

Run this from the repo root, replacing the skill name if needed:

```powershell
$skill = "brand-pizza-hips-guidelines"
$src = (Resolve-Path "skill-drafts\$skill").Path
$out = Join-Path (Resolve-Path "skill-drafts") "$skill.zip"

if (Test-Path -LiteralPath $out) {
  Remove-Item -LiteralPath $out
}

Add-Type -AssemblyName System.IO.Compression
Add-Type -AssemblyName System.IO.Compression.FileSystem

$zip = [System.IO.Compression.ZipFile]::Open(
  $out,
  [System.IO.Compression.ZipArchiveMode]::Create
)

try {
  $prefix = $src.TrimEnd('\') + '\'
  Get-ChildItem -LiteralPath $src -Recurse -File |
    Sort-Object FullName |
    ForEach-Object {
      $rel = $_.FullName.Substring($prefix.Length).Replace('\', '/')
      [System.IO.Compression.ZipFileExtensions]::CreateEntryFromFile(
        $zip,
        $_.FullName,
        $rel,
        [System.IO.Compression.CompressionLevel]::Optimal
      ) | Out-Null
    }
} finally {
  $zip.Dispose()
}
```

## Verify ZIP Entries

After packaging, inspect the ZIP entries:

```powershell
$zipPath = "skill-drafts\brand-pizza-hips-guidelines.zip"

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

Expected output for the Pizza Hip'S brand skill:

```text
FullName                    Length
--------                    ------
assets/fonts/SVN-Bango.otf   29792
SKILL.md                     12303

OK: no backslash entries
```

## Font Hash Check

For `brand-pizza-hips-guidelines`, the expected font hash is:

```text
0C72A0D3D2A61E550E24A35FBF41F0DE3517026F09526F5D8CAF1BBF963FA5D9
```

Verify it with:

```powershell
Get-FileHash -Algorithm SHA256 `
  "skill-drafts\brand-pizza-hips-guidelines\assets\fonts\SVN-Bango.otf"
```

