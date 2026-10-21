# Renders five candidate designs for a generic, release-independent goSSMS
# LinkedIn banner (1200 x 627), plus an overview sheet for choosing between them.
#
#   powershell -NoProfile -File proposal\generate_proposals.ps1
#   powershell -NoProfile -File proposal\generate_proposals.ps1 -Version v0.1.1
#
# The version string is the only per-release input. Background is
# screen\08_exec_plan_graph.png; the icon is the shipping app icon,
# ico\14_breakout_ssms_1024.png. gossms_logo.png is deliberately not used: it
# omits Windows. Requires Montserrat (falls back to Segoe UI) and Consolas.

param([string]$Version = 'v0.1.0')

$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Drawing

$OutDir = $PSScriptRoot
$Root = Split-Path $PSScriptRoot -Parent
$ShotPath = Join-Path $Root 'screen\08_exec_plan_graph.png'
$IconPath = Join-Path $Root 'ico\14_breakout_ssms_1024.png'

$W = 1200
$H = 627

# ---------------------------------------------------------------- fonts ------
$pfc = New-Object System.Drawing.Text.PrivateFontCollection
$fontDirs = @(
  (Join-Path $env:LOCALAPPDATA 'Microsoft\Windows\Fonts'),
  (Join-Path $env:WINDIR 'Fonts')
)
foreach ($f in @('Montserrat-Bold.ttf', 'Montserrat-Regular.ttf')) {
  foreach ($d in $fontDirs) {
    $p = Join-Path $d $f
    if (Test-Path $p) { $pfc.AddFontFile($p); break }
  }
}
$mont = $pfc.Families | Where-Object { $_.Name -like 'Montserrat*' } | Select-Object -First 1
if (-not $mont) {
  Write-Warning 'Montserrat not found; falling back to Segoe UI - check the layout.'
  $mont = New-Object System.Drawing.FontFamily('Segoe UI')
}

function Bold([single]$size) {
  New-Object System.Drawing.Font($mont, $size, [System.Drawing.FontStyle]::Bold)
}
function Reg([single]$size) {
  if ($mont.IsStyleAvailable([System.Drawing.FontStyle]::Regular)) {
    New-Object System.Drawing.Font($mont, $size, [System.Drawing.FontStyle]::Regular)
  } else { Bold $size }
}
function Mono([single]$size, [switch]$B) {
  $st = if ($B) { [System.Drawing.FontStyle]::Bold } else { [System.Drawing.FontStyle]::Regular }
  New-Object System.Drawing.Font('Consolas', $size, $st)
}

# --------------------------------------------------------------- helpers -----
# Shared with res_v0.0.11\generate_images.ps1.
function C([string]$hex) { [System.Drawing.ColorTranslator]::FromHtml($hex) }
function A([int]$alpha, [string]$hex) {
  $c = C $hex
  [System.Drawing.Color]::FromArgb($alpha, $c.R, $c.G, $c.B)
}
function Br($color) { New-Object System.Drawing.SolidBrush($color) }

$SF = [System.Drawing.StringFormat]::GenericTypographic.Clone()
$SF.FormatFlags = $SF.FormatFlags -bor [System.Drawing.StringFormatFlags]::MeasureTrailingSpaces

function TextW($g, [string]$s, $font) {
  if ([string]::IsNullOrEmpty($s)) { return 0 }
  $g.MeasureString($s, $font, [System.Drawing.PointF]::Empty, $SF).Width
}

function Say($g, [string]$s, $font, $color, [single]$x, [single]$y) {
  $b = Br $color
  $g.DrawString($s, $font, $b, (New-Object System.Drawing.PointF($x, $y)), $SF)
  $b.Dispose()
}
function SayCenter($g, [string]$s, $font, $color, [single]$y) {
  Say $g $s $font $color (($W - (TextW $g $s $font)) / 2) $y
}
function SayRight($g, [string]$s, $font, $color, [single]$xr, [single]$y) {
  Say $g $s $font $color ($xr - (TextW $g $s $font)) $y
}

function SayTracked($g, [string]$s, $font, $color, [single]$x, [single]$y, [single]$track) {
  $b = Br $color
  $cx = $x
  foreach ($ch in $s.ToCharArray()) {
    $str = [string]$ch
    $g.DrawString($str, $font, $b, (New-Object System.Drawing.PointF($cx, $y)), $SF)
    $cx += (TextW $g $str $font) + $track
  }
  $b.Dispose()
  return $cx - $track - $x
}
function TrackedW($g, [string]$s, $font, [single]$track) {
  $tw = 0
  foreach ($ch in $s.ToCharArray()) { $tw += (TextW $g ([string]$ch) $font) + $track }
  return $tw - $track
}

function RoundPath([single]$x, [single]$y, [single]$w, [single]$h, [single]$r) {
  $p = New-Object System.Drawing.Drawing2D.GraphicsPath
  if ($r -le 0) { $p.AddRectangle((New-Object System.Drawing.RectangleF($x, $y, $w, $h))); return $p }
  $d = $r * 2
  $p.AddArc($x, $y, $d, $d, 180, 90)
  $p.AddArc($x + $w - $d, $y, $d, $d, 270, 90)
  $p.AddArc($x + $w - $d, $y + $h - $d, $d, $d, 0, 90)
  $p.AddArc($x, $y + $h - $d, $d, $d, 90, 90)
  $p.CloseFigure()
  return $p
}

function Shadow($g, [single]$x, [single]$y, [single]$w, [single]$h, [single]$r,
                [int]$spread = 16, [int]$alpha = 13, [single]$dy = 7, [string]$hex = '#00050F') {
  for ($i = $spread; $i -ge 1; $i--) {
    $p = RoundPath ($x - $i) ($y - $i + $dy) ($w + 2 * $i) ($h + 2 * $i) ($r + $i)
    $b = Br (A $alpha $hex)
    $g.FillPath($b, $p)
    $b.Dispose(); $p.Dispose()
  }
}

function Card($g, [single]$x, [single]$y, [single]$w, [single]$h, [single]$r, $fill,
              $border = $null, [single]$bw = 1) {
  $p = RoundPath $x $y $w $h $r
  $b = Br $fill
  $g.FillPath($b, $p)
  $b.Dispose()
  if ($border) {
    $pen = New-Object System.Drawing.Pen($border, $bw)
    $g.DrawPath($pen, $p)
    $pen.Dispose()
  }
  $p.Dispose()
}

# Rounded pill sized to its label; returns the pill width. Optional border.
function PillW($g, [string]$text, $font, [single]$padX, [single]$track = 0) {
  $tw = if ($track -gt 0) { TrackedW $g $text $font $track } else { TextW $g $text $font }
  return $tw + 2 * $padX
}
function Pill($g, [string]$text, $font, $fill, $textColor, [single]$x, [single]$y,
              [single]$h, [single]$padX, [single]$track = 0, $border = $null, [single]$bw = 1.5) {
  $pw = PillW $g $text $font $padX $track
  Card $g $x $y $pw $h ($h / 2) $fill $border $bw
  $ty = $y + ($h - $font.GetHeight($g)) / 2
  if ($track -gt 0) { [void](SayTracked $g $text $font $textColor ($x + $padX) $ty $track) }
  else { Say $g $text $font $textColor ($x + $padX) $ty }
  return $pw
}

function Gradient($g, [single]$x, [single]$y, [single]$w, [single]$h, $c1, $c2, [single]$angle) {
  $rect = New-Object System.Drawing.RectangleF($x, $y, $w, $h)
  $b = New-Object System.Drawing.Drawing2D.LinearGradientBrush($rect, $c1, $c2, $angle)
  $g.FillRectangle($b, $rect)
  $b.Dispose()
}

# Soft elliptical glow, transparent at the rim.
function Glow($g, [single]$cx, [single]$cy, [single]$rx, [single]$ry, $color) {
  $p = New-Object System.Drawing.Drawing2D.GraphicsPath
  $p.AddEllipse(($cx - $rx), ($cy - $ry), (2 * $rx), (2 * $ry))
  $b = New-Object System.Drawing.Drawing2D.PathGradientBrush($p)
  $b.CenterColor = $color
  $b.SurroundColors = [System.Drawing.Color[]]@([System.Drawing.Color]::FromArgb(0, $color))
  $g.FillPath($b, $p)
  $b.Dispose(); $p.Dispose()
}

function NewCanvas([int]$cw = $W, [int]$ch = $H) {
  $bmp = New-Object System.Drawing.Bitmap($cw, $ch, [System.Drawing.Imaging.PixelFormat]::Format32bppArgb)
  $g = [System.Drawing.Graphics]::FromImage($bmp)
  $g.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::AntiAlias
  $g.TextRenderingHint = [System.Drawing.Text.TextRenderingHint]::AntiAliasGridFit
  $g.InterpolationMode = [System.Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
  $g.PixelOffsetMode = [System.Drawing.Drawing2D.PixelOffsetMode]::HighQuality
  $g.CompositingQuality = [System.Drawing.Drawing2D.CompositingQuality]::HighQuality
  return @($bmp, $g)
}

function Save($bmp, [string]$name) {
  $bmp.Save((Join-Path $OutDir $name), [System.Drawing.Imaging.ImageFormat]::Png)
  Write-Host "wrote $name"
}

# Draw the screenshot to fill a box (cover). The focus box, in fractions of the
# source, picks the region to crop from; -TopAnchor keeps the top of the focus.
# Integer rectangles throughout, so PowerShell resolves the ImageAttributes overload.
function DrawShot($g, $img, [int]$dx, [int]$dy, [int]$dw, [int]$dh, $attr = $null,
                  [double]$fx = 0, [double]$fy = 0, [double]$fw = 1, [double]$fh = 1,
                  [switch]$TopAnchor) {
  $sx0 = $img.Width * $fx; $sy0 = $img.Height * $fy
  $sw0 = $img.Width * $fw; $sh0 = $img.Height * $fh
  $aspect = $dw / $dh
  if ($sw0 / $sh0 -gt $aspect) { $sh = $sh0; $sw = $sh * $aspect } else { $sw = $sw0; $sh = $sw / $aspect }
  $sx = $sx0 + ($sw0 - $sw) / 2
  $sy = if ($TopAnchor) { $sy0 } else { $sy0 + ($sh0 - $sh) / 2 }
  if (-not $attr) { $attr = New-Object System.Drawing.Imaging.ImageAttributes }
  $attr.SetWrapMode([System.Drawing.Drawing2D.WrapMode]::TileFlipXY)
  $dest = New-Object System.Drawing.Rectangle($dx, $dy, $dw, $dh)
  $g.DrawImage($img, $dest, [int]$sx, [int]$sy, [int]$sw, [int]$sh, [System.Drawing.GraphicsUnit]::Pixel, $attr)
}

# Cheap blur: shrink, then let the bicubic upscale in DrawShot smear it back.
function Shrunk($img, [double]$factor) {
  $bw = [int]($img.Width / $factor); $bh = [int]($img.Height / $factor)
  $r = NewCanvas $bw $bh
  $r[1].DrawImage($img, (New-Object System.Drawing.Rectangle(0, 0, $bw, $bh)))
  $r[1].Dispose()
  return $r[0]
}

# Maps luminance onto a dark->light colour ramp.
function Duotone([string]$darkHex, [string]$lightHex) {
  $d = C $darkHex; $l = C $lightHex
  $m = New-Object System.Drawing.Imaging.ColorMatrix
  $lum = @(0.299, 0.587, 0.114)
  $dc = @($d.R, $d.G, $d.B); $lc = @($l.R, $l.G, $l.B)
  for ($j = 0; $j -lt 3; $j++) {
    $span = ($lc[$j] - $dc[$j]) / 255.0
    for ($i = 0; $i -lt 3; $i++) { $m.Item($i, $j) = [single]($lum[$i] * $span) }
    $m.Item(4, $j) = [single]($dc[$j] / 255.0)
  }
  $m.Item(3, 3) = 1; $m.Item(4, 4) = 1
  $a = New-Object System.Drawing.Imaging.ImageAttributes
  $a.SetColorMatrix($m)
  return $a
}

function DrawIcon($g, [single]$x, [single]$y, [single]$size, [int]$shadowAlpha = 16) {
  Shadow $g ($x + $size * 0.04) ($y + $size * 0.04) ($size * 0.92) ($size * 0.92) ($size * 0.2) `
         ([int]($size * 0.12)) $shadowAlpha ($size * 0.05)
  $g.DrawImage($icon, $x, $y, $size, $size)
}

# "goSSMS" set in two colours, as on the logo. Returns its width.
function BrandW($g, $font) { (TextW $g 'go' $font) + (TextW $g 'SSMS' $font) }
function Brand($g, [single]$x, [single]$y, $font, $goColor, $ssmsColor) {
  Say $g 'go' $font $goColor $x $y
  Say $g 'SSMS' $font $ssmsColor ($x + (TextW $g 'go' $font)) $y
}

function TrafficLights($g, [single]$x, [single]$cy, [single]$r) {
  $cols = @('#FF5F57', '#FEBC2E', '#28C840')
  for ($i = 0; $i -lt 3; $i++) {
    $b = Br (C $cols[$i])
    $g.FillEllipse($b, ($x + $i * $r * 3.3), ($cy - $r), (2 * $r), (2 * $r))
    $b.Dispose()
  }
}

# ---------------------------------------------------------------- content ----
# Generic on purpose: no feature list, so only $Version changes per release.
$TAGLINE = 'SQL Server management, right in your terminal.'
$PLATFORMS = @('macOS', 'Linux', 'Windows')
$URL = 'github.com/radix29/gossms'
$DOT = '   ' + [char]0x00B7 + '   '

$BLUE = '#1668D8'; $BLUE_D = '#0B4FA8'; $SKY = '#5FA8FF'
$NAVY = '#07111F'; $GREEN = '#3FD37A'
$WHITE = C '#FFFFFF'

$shot = [System.Drawing.Image]::FromFile($ShotPath)
$icon = [System.Drawing.Image]::FromFile($IconPath)
$outputs = @()

# ======================================================= 01 dark veil =========
# Full-bleed screenshot; navy veil on the right carries right-aligned brand, so
# the Object Explorer tree and the left of the plan graph stay visible.
$r = NewCanvas; $bmp = $r[0]; $g = $r[1]
DrawShot $g $shot 0 0 $W $H
$vb = Br (A 120 $NAVY); $g.FillRectangle($vb, 0, 0, 262, $H); $vb.Dispose()
Gradient $g 261 0 380 $H (A 120 $NAVY) (A 246 $NAVY) 0
$vb = Br (A 246 $NAVY); $g.FillRectangle($vb, 640, 0, ($W - 640), $H); $vb.Dispose()
Gradient $g 0 430 $W 197 (A 0 $NAVY) (A 200 $NAVY) 90
Glow $g 1080 120 220 180 (A 70 $BLUE)

$XR = 1136
DrawIcon $g ($XR - 116) 60 116
$bf = Bold 68
Brand $g ($XR - (BrandW $g $bf)) 200 $bf (C $SKY) $WHITE
SayRight $g $TAGLINE (Reg 20) (C '#C9DBF2') $XR 300

$pl = $PLATFORMS -join $DOT; $plf = Bold 18
$plw = TextW $g $pl $plf
$ow = PillW $g 'OPEN SOURCE' (Bold 13) 16 1.6
[void](Pill $g 'OPEN SOURCE' (Bold 13) (C $GREEN) (C $NAVY) ($XR - $plw - 16 - $ow) 352 36 16 1.6)
Say $g $pl $plf $WHITE ($XR - $plw) 359

SayRight $g $Version (Bold 56) $WHITE ($XR + 4) 466
SayRight $g $URL (Mono 15) (C '#C9DBF2') $XR 580
Save $bmp 'proposal_01_veil.png'; $outputs += 'proposal_01_veil.png'
$g.Dispose(); $bmp.Dispose()

# ===================================================== 02 centred glass ========
# Darkened screenshot, everything centred over a blue glow. 02 softens the
# screenshot slightly; 02b keeps it sharp under a heavier veil.
function Centred([double]$blur, [int]$veilTop, [int]$veilBottom, [string]$name) {
  $r = NewCanvas; $bmp = $r[0]; $g = $r[1]
  if ($blur -gt 1) {
    $soft = Shrunk $shot $blur
    DrawShot $g $soft 0 0 $W $H
    $soft.Dispose()
  } else { DrawShot $g $shot 0 0 $W $H }
  Gradient $g 0 0 $W $H (A $veilTop '#06101D') (A $veilBottom '#06101D') 90
  Glow $g 600 200 460 260 (A 95 $BLUE)
  Glow $g 600 490 420 110 (A 255 '#06101D'); Glow $g 600 490 300 60 (A 200 '#06101D')

  DrawIcon $g 536 50 128 22
  $bf = Bold 64
  Brand $g (($W - (BrandW $g $bf)) / 2) 196 $bf (C $SKY) $WHITE
  SayCenter $g $TAGLINE (Reg 20) (C '#C9DBF2') 288

  $vf = Bold 24
  $vw = PillW $g $Version $vf 34
  [void](Pill $g $Version $vf $WHITE (C $BLUE_D) (($W - $vw) / 2) 344 56 34)

  $ot = 'OPEN SOURCE'; $of = Bold 13
  [void](SayTracked $g $ot $of (C $GREEN) (($W - (TrackedW $g $ot $of 3)) / 2) 452 3)
  SayCenter $g ($PLATFORMS -join $DOT) (Bold 21) (C '#E6EEF8') 478
  SayCenter $g $URL (Mono 15) (C '#8FA9C8') 572
  Save $bmp $name
  $g.Dispose(); $bmp.Dispose()
}
Centred 3 180 220 'proposal_02_centred.png'; $outputs += 'proposal_02_centred.png'
Centred 1 200 232 'proposal_02b_sharp.png'; $outputs += 'proposal_02b_sharp.png'

# ====================================================== 03 blueprint band ======
# Screenshot as a blue duotone "blueprint"; white band carries the brand.
$r = NewCanvas; $bmp = $r[0]; $g = $r[1]
$duo = Duotone '#06204A' '#9CCBFF'
DrawShot $g $shot 0 0 $W 440 $duo 0 0 1 1 -TopAnchor
Gradient $g 0 300 $W 140 (A 0 '#06204A') (A 150 '#06204A') 90
$wb = Br $WHITE; $g.FillRectangle($wb, 0, 427, $W, 200); $wb.Dispose()
$bb = Br (C $BLUE); $g.FillRectangle($bb, 0, 427, $W, 6); $bb.Dispose()

DrawIcon $g 64 322 156 30
Brand $g 246 448 (Bold 50) (C $BLUE) (C '#0B2038')
Say $g $TAGLINE (Reg 17) (C '#4A6380') 250 520

$px = 250
$px += (Pill $g 'OPEN SOURCE' (Bold 12) (C $GREEN) (C $NAVY) $px 558 30 13 1.4) + 12
foreach ($pl in $PLATFORMS) {
  $px += (Pill $g $pl (Bold 13) (C '#EEF4FC') (C $BLUE_D) $px 558 30 14 0 (C '#BFD5F0') 1.2) + 10
}

SayRight $g $Version (Bold 60) (C $BLUE) 1140 462
SayRight $g $URL (Mono 14) (C '#7A8FA6') 1136 574
Save $bmp 'proposal_03_blueprint.png'; $outputs += 'proposal_03_blueprint.png'
$g.Dispose(); $bmp.Dispose()

# ====================================================== 04 terminal window =====
# The whole banner is a full-bleed macOS Terminal window running goSSMS:
# title bar along the top edge, no surrounding frame; brand fades in below.
$r = NewCanvas; $bmp = $r[0]; $g = $r[1]
# desktop behind the window, visible only in the two rounded top corners
Gradient $g 0 0 $W $H (C '#2E86E8') (C '#0B3F8F') 60
$tb = 40; $cr = 16
# window shape: rounded top corners, square bottom (it runs off the banner)
$wp = New-Object System.Drawing.Drawing2D.GraphicsPath
$wp.AddArc(0, 0, (2 * $cr), (2 * $cr), 180, 90)
$wp.AddArc(($W - 2 * $cr), 0, (2 * $cr), (2 * $cr), 270, 90)
$wp.AddLine($W, $cr, $W, $H)
$wp.AddLine($W, $H, 0, $H)
$wp.CloseFigure()
$st = $g.Save(); $g.SetClip($wp)
$tbb = Br (C '#1D3A5C'); $g.FillRectangle($tbb, 0, 0, $W, $tb); $tbb.Dispose()
TrafficLights $g 22 ($tb / 2) 7
$tt = 'goSSMS ' + [char]0x2014 + ' ' + $Version; $tf = Reg 14
Say $g $tt $tf (C '#B7C9DE') (($W - (TextW $g $tt $tf)) / 2) 11
DrawShot $g $shot 0 $tb $W ([int]($W * $shot.Height / $shot.Width)) $null 0 0 1 1 -TopAnchor
$g.Restore($st)
# faint top-edge highlight, as on a macOS window
$hp = New-Object System.Drawing.Drawing2D.GraphicsPath
$hp.AddArc(0.5, 0.5, (2 * $cr), (2 * $cr), 180, 90)
$hp.AddArc(($W - 2 * $cr - 0.5), 0.5, (2 * $cr), (2 * $cr), 270, 90)
$pen = New-Object System.Drawing.Pen((A 70 '#FFFFFF'), 1); $g.DrawPath($pen, $hp); $pen.Dispose()
$hp.Dispose(); $wp.Dispose()

Gradient $g 0 220 $W 201 (A 0 '#061428') (A 246 '#061428') 90
$vb = Br (A 246 '#061428'); $g.FillRectangle($vb, 0, 420, $W, ($H - 420)); $vb.Dispose()
DrawIcon $g 92 432 112 24
Brand $g 228 426 (Bold 54) (C $SKY) $WHITE
Say $g $TAGLINE (Reg 18) (C '#C9DBF2') 232 502

$px = 232
$px += (Pill $g 'OPEN SOURCE' (Bold 12) (C $GREEN) (C $NAVY) $px 546 30 13 1.4) + 18
Say $g ($PLATFORMS -join $DOT) (Bold 16) $WHITE $px 551

$vf = Bold 26
$vw = PillW $g $Version $vf 28
[void](Pill $g $Version $vf $WHITE (C $BLUE_D) (1108 - $vw) 452 58 28)
SayRight $g $URL (Mono 14) (C '#8FA9C8') 1108 558
Save $bmp 'proposal_04_window.png'; $outputs += 'proposal_04_window.png'
$g.Dispose(); $bmp.Dispose()

# ====================================================== 05 plan tree ===========
# Zoomed into the plan graph; the brand facts are drawn as plan operators.
$r = NewCanvas; $bmp = $r[0]; $g = $r[1]
DrawShot $g $shot 0 0 $W $H $null 0.195 0.2 0.8 0.78
Gradient $g 0 0 $W $H (A 232 '#050D18') (A 200 '#050D18') 0
Gradient $g 0 0 $W 180 (A 235 '#050D18') (A 0 '#050D18') 90
Glow $g 1000 300 300 260 (A 70 $BLUE)

Brand $g 56 46 (Bold 52) (C $SKY) $WHITE
Say $g $TAGLINE (Reg 18) (C '#AFC6E2') 62 116

$node = A 238 '#0C1B2E'
$line = C '#7FA7D9'
# root operator, highlighted like the selected node in the screenshot
Card $g 60 258 330 170 4 $node (C '#2E86E8') 2.5
Card $g 66 264 318 158 2 (A 0 '#000000') (A 120 '#2E86E8') 1
Say $g 'goSSMS' (Mono 18) (C '#E6EEF8') 84 276
Say $g $Version (Mono 46 -B) $WHITE 80 306
Say $g 'Open source' (Mono 16) (C $GREEN) 84 382

$kids = @(
  @{ Name = 'macOS';   Sub = 'Apple silicon ' + [char]0x00B7 + ' Intel' },
  @{ Name = 'Linux';   Sub = 'amd64 ' + [char]0x00B7 + ' arm64' },
  @{ Name = 'Windows'; Sub = 'amd64' }
)
$pen = New-Object System.Drawing.Pen($line, 2)
$g.DrawLine($pen, 404, 343, 455, 343)
$g.DrawLine($pen, 455, 239, 455, 447)
for ($i = 0; $i -lt 3; $i++) {
  $ky = 196 + $i * 104
  $g.DrawLine($pen, 455, ($ky + 43), 520, ($ky + 43))
  Card $g 520 $ky 290 86 4 $node (C '#5D7893') 1.5
  Say $g $kids[$i].Name (Mono 20) $WHITE 538 ($ky + 14)
  Say $g $kids[$i].Sub (Mono 15) (C '#8FB6E8') 538 ($ky + 50)
}
$pen.Dispose()
# arrowhead into the root, as plan edges point toward their consumer
$ab = Br $line
$g.FillPolygon($ab, [System.Drawing.PointF[]]@(
  (New-Object System.Drawing.PointF(392, 343)),
  (New-Object System.Drawing.PointF(406, 336)),
  (New-Object System.Drawing.PointF(406, 350))))
$ab.Dispose()

DrawIcon $g 890 196 220 26

# status bar, as at the bottom of the goSSMS window
$sb = Br (C $BLUE); $g.FillRectangle($sb, 0, 597, $W, 30); $sb.Dispose()
$sep = '  >  '
Say $g ('goSSMS' + $sep + 'Releases' + $sep + $Version) (Mono 15) $WHITE 16 603
SayRight $g $URL (Mono 15) $WHITE 1184 603
Save $bmp 'proposal_05_plan.png'; $outputs += 'proposal_05_plan.png'
$g.Dispose(); $bmp.Dispose()

# ======================================================== overview sheet =======
$tw = 580; $th = [int]($tw * $H / $W); $m = 20; $lab = 30
$rows = [Math]::Ceiling($outputs.Count / 2)
$r = NewCanvas ($m * 3 + $tw * 2) ($m + $rows * ($lab + $th + $m)); $bmp = $r[0]; $g = $r[1]
$g.Clear((C '#E9EEF4'))
$labels = @('01  Dark veil', '02  Centred, light blur', '02b  Centred, no blur', '03  Blueprint band', '04  Terminal window', '05  Plan tree')
for ($i = 0; $i -lt $outputs.Count; $i++) {
  $cx = $m + ($i % 2) * ($tw + $m)
  $cy = $m + [Math]::Floor($i / 2) * ($lab + $th + $m)
  Say $g $labels[$i] (Bold 14) (C '#0B2038') $cx ($cy + 4)
  $img = [System.Drawing.Image]::FromFile((Join-Path $OutDir $outputs[$i]))
  $g.DrawImage($img, (New-Object System.Drawing.Rectangle([int]$cx, [int]($cy + $lab), $tw, $th)))
  $img.Dispose()
}
Save $bmp 'overview.png'
$g.Dispose(); $bmp.Dispose()

$shot.Dispose(); $icon.Dispose()
Write-Host 'done'
