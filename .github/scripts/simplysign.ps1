<#
simplysign.ps1: unattended SimplySign Desktop login for the EDSense sign job.

Dot-source it, then call the functions:
  . ./.github/scripts/simplysign.ps1
  Install-SimplySign                      # download, check and install the pinned MSI
  Connect-SimplySign -Thumbprint <sha1>   # log in; reads CERTUM_USERNAME and CERTUM_OTP_URI
  Invoke-CodeSign -Unsigned a.exe -Signed b.exe -Thumbprint <sha1>
  Test-CodeSignature -Path b.exe -Thumbprint <sha1>
  Disconnect-SimplySign                   # stop the app, clear the clipboard
  Test-Totp                               # RFC 6238 / RFC 4226 self-test, no network
Or run it with -SelfTest for the TOTP self-test alone.

Runs in PowerShell 7 (pwsh on the runner). The TOTP code and the self-test also
run in Windows PowerShell 5.1.

The repo is public and so are its Actions logs. Nothing here prints the TOTP
secret, a code, the e-mail, the text of a login field or a line of the
SimplySign app log; derived values are masked with ::add-mask::.

sign.yml pins the SHA-256 of this file (PIN_SIMPLYSIGN_PS1) and checks it
before anything here runs. Change the pin in the same commit as this file.

Ideas taken from these public projects (no code copied):
  dismine/windows-app-signing-setup-action (MIT): registry preset, clipboard
    paste, retry only in a fresh time step, SHA256 for Certum, Yes/No update
    prompt
  FrodeHus/elevate, russmckendrick/azdocs (MIT): one launch, one login at a
    time, never answer Yes to the update prompt, a session lasts 2 hours
  Rumia-Channel/windows-certum-signin (MIT)
  jay0lee/certum-cloud-code-sign (Apache-2.0): second launch, timings,
    SHA-256 TOTP for Certum
  Takuya Matsuyama, "How to automate signing your Windows app with Certum"
    (devas.life)
#>
[CmdletBinding()]
param([switch]$SelfTest)

$SsdConfig = @{
    # pinned on purpose; bump the version, size and SIMPLYSIGN_MSI_SHA256 together
    MsiUrl         = 'https://files.certum.eu/software/SimplySignDesktop/Windows/9.4.5.95/SimplySignDesktop-9.4.5.95-64-bit-en.msi'
    MsiSize        = [long]279712768
    SignerName     = 'Asseco Data Systems S.A.'
    IssuerName     = 'Certum Extended Validation Code Signing 2021 CA'
    Exe            = "$env:ProgramFiles\Certum\SimplySign Desktop\SimplySignDesktop.exe"
    ProcessName    = 'SimplySignDesktop'
    RegistryKey    = 'HKCU:\Software\Certum\SimplySign'
    TimestampUrl   = 'http://time.certum.pl'
    MinSecondsLeft = 20
}

# ---------------------------------------------------------------- TOTP

function Add-ActionsMask {
    # hide a value in the Actions log; the runner only masks exact secret values
    param([string]$Value)
    if ($env:GITHUB_ACTIONS -eq 'true' -and -not [string]::IsNullOrEmpty($Value)) {
        Write-Host ('::add-mask::' + $Value.Replace('%', '%25').Replace("`r", '%0D').Replace("`n", '%0A'))
    }
}

function ConvertFrom-Base32 {
    # RFC 4648 base32; padding optional, case-insensitive, spaces and dashes ignored
    param([Parameter(Mandatory = $true)][string]$Text)
    $alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567'
    $clean = ($Text -replace '[\s=-]', '').ToUpperInvariant()
    if ($clean.Length -eq 0) { throw 'The TOTP secret is empty.' }
    if (@(1, 3, 6) -contains ($clean.Length % 8)) { throw 'The TOTP secret has an impossible base32 length.' }
    $out = New-Object 'System.Collections.Generic.List[byte]'
    $buffer = 0
    $bits = 0
    foreach ($ch in $clean.ToCharArray()) {
        $v = $alphabet.IndexOf($ch)
        # never name the bad character: it is part of the secret
        if ($v -lt 0) { throw 'The TOTP secret is not valid base32.' }
        $buffer = (($buffer -shl 5) -bor $v) -band 0xFFFF
        $bits += 5
        if ($bits -ge 8) {
            $bits -= 8
            $out.Add([byte](($buffer -shr $bits) -band 0xFF))
        }
    }
    return , $out.ToArray()
}

function Get-TotpCode {
    # RFC 6238 code for a raw key; returns the code, prints nothing
    param(
        [Parameter(Mandatory = $true)][byte[]]$Key,
        [Parameter(Mandatory = $true)][long]$UnixTime,
        [int]$Period = 30,
        [int]$Digits = 6,
        [ValidateSet('SHA1', 'SHA256', 'SHA512')][string]$Algorithm = 'SHA1'
    )
    if ($Period -le 0) { throw 'The TOTP period must be positive.' }
    if ($Digits -lt 6 -or $Digits -gt 8) { throw 'TOTP digits must be 6 to 8.' }
    $rem = [long]0
    $counter = [Math]::DivRem([long]$UnixTime, [long]$Period, [ref]$rem)
    $msg = [BitConverter]::GetBytes([long]$counter)
    if ([BitConverter]::IsLittleEndian) { [Array]::Reverse($msg) }   # 8-byte big-endian counter
    switch ($Algorithm) {
        'SHA1' { $hmac = New-Object System.Security.Cryptography.HMACSHA1 -ArgumentList (, $Key) }
        'SHA256' { $hmac = New-Object System.Security.Cryptography.HMACSHA256 -ArgumentList (, $Key) }
        'SHA512' { $hmac = New-Object System.Security.Cryptography.HMACSHA512 -ArgumentList (, $Key) }
    }
    try { $hash = $hmac.ComputeHash($msg) } finally { $hmac.Dispose() }
    $o = $hash[$hash.Length - 1] -band 0x0F                            # dynamic truncation
    $bin = (([int]$hash[$o] -band 0x7F) -shl 24) -bor ([int]$hash[$o + 1] -shl 16) -bor ([int]$hash[$o + 2] -shl 8) -bor [int]$hash[$o + 3]
    $mod = [long]1
    for ($i = 0; $i -lt $Digits; $i++) { $mod *= 10 }
    return ([long]$bin % $mod).ToString([Globalization.CultureInfo]::InvariantCulture).PadLeft($Digits, '0')
}

function ConvertFrom-OtpAuthUri {
    # otpauth://totp/...?secret=...&algorithm=...&digits=...&period=...
    # Defaults follow the Key URI Format: SHA1, 6 digits, 30 s. DefaultAlgorithm
    # (CERTUM_TOTP_ALGORITHM) is used when the URI has no algorithm=.
    param([Parameter(Mandatory = $true)][string]$Uri, [string]$DefaultAlgorithm)
    $u = $Uri.Trim()
    if ($u -notmatch '^otpauth://totp/[^?]*\?(?<q>.+)$') { throw 'The OTP URI is not an otpauth://totp/ URI.' }
    $query = $Matches['q']
    $q = @{}
    foreach ($pair in $query.Split([char[]]@('&'))) {
        $kv = $pair.Split([char[]]@('='), 2)
        if ($kv.Count -ne 2) { continue }
        $name = [Uri]::UnescapeDataString($kv[0]).ToLowerInvariant()
        $value = [Uri]::UnescapeDataString($kv[1])
        if ($name -eq 'secret') {
            Add-ActionsMask $kv[1]
            Add-ActionsMask $value
        }
        $q[$name] = $value
    }
    $secret = [string]$q['secret']
    if (-not $secret) { throw 'The OTP URI has no secret.' }
    $normal = ($secret -replace '[\s=-]', '')
    Add-ActionsMask $normal.ToUpperInvariant()
    Add-ActionsMask $normal.ToLowerInvariant()

    $def = ([string]$DefaultAlgorithm).Trim().ToUpperInvariant().Replace('-', '')
    $alg = ([string]$q['algorithm']).Trim().ToUpperInvariant().Replace('-', '')
    $source = 'uri'
    if (-not $alg) {
        if ($def) { $alg = $def; $source = 'variable' } else { $alg = 'SHA1'; $source = 'default' }
    }
    if ($def -and $def -ne $alg) { throw 'CERTUM_TOTP_ALGORITHM disagrees with algorithm= in the OTP URI.' }
    if (@('SHA1', 'SHA256', 'SHA512') -notcontains $alg) { throw "Unsupported TOTP algorithm '$alg' (SHA1, SHA256 or SHA512)." }

    [int]$digits = 6
    [int]$period = 30
    if ($q.ContainsKey('digits') -and -not [int]::TryParse($q['digits'], [ref]$digits)) { throw 'digits= in the OTP URI is not a number.' }
    if ($q.ContainsKey('period') -and -not [int]::TryParse($q['period'], [ref]$period)) { throw 'period= in the OTP URI is not a number.' }
    if ($digits -lt 6 -or $digits -gt 8) { throw "Unsupported TOTP digits $digits (6 to 8)." }
    if ($period -lt 15 -or $period -gt 120) { throw "Unsupported TOTP period $period (15 to 120 s)." }

    [pscustomobject]@{
        Key             = (ConvertFrom-Base32 $secret)
        Algorithm       = $alg
        AlgorithmSource = $source
        Digits          = $digits
        Period          = $period
    }
}

function Get-Totp {
    # The current code for an otpauth URI. Returns the code (masked in Actions)
    # and prints nothing; never write the result to the log. -NoMask is for
    # the published RFC vectors only: a mask of those digits would also hide
    # them in hashes and ids, and the runner drops a job output that holds one.
    param(
        [Parameter(Mandatory = $true)][string]$Uri,
        [string]$DefaultAlgorithm = $env:CERTUM_TOTP_ALGORITHM,
        [long]$UnixTime = -1,
        [switch]$NoMask
    )
    $otp = ConvertFrom-OtpAuthUri -Uri $Uri -DefaultAlgorithm $DefaultAlgorithm
    try {
        if ($UnixTime -lt 0) { $UnixTime = [DateTimeOffset]::UtcNow.ToUnixTimeSeconds() }
        $code = Get-TotpCode -Key $otp.Key -UnixTime $UnixTime -Period $otp.Period -Digits $otp.Digits -Algorithm $otp.Algorithm
        if (-not $NoMask) { Add-ActionsMask $code }
        return $code
    } finally {
        [Array]::Clear($otp.Key, 0, $otp.Key.Length)
    }
}

function Test-Totp {
    # RFC 6238 Appendix B and RFC 4226 Appendix D vectors, base32 and URI cases.
    # Returns $true when all pass. Uses only the published test seeds.
    [CmdletBinding()]
    param()
    Set-StrictMode -Version 2
    $ErrorActionPreference = 'Stop'
    $r = @{ Pass = 0; Fail = 0 }
    $failed = New-Object 'System.Collections.Generic.List[string]'
    $check = {
        param([bool]$Ok, [string]$What)
        if ($Ok) { $r.Pass++ } else { $r.Fail++; $failed.Add($What) }
    }
    $ascii = [Text.Encoding]::ASCII
    $seed = @{
        SHA1   = '12345678901234567890'
        SHA256 = '12345678901234567890123456789012'
        SHA512 = ('1234567890' * 6) + '1234'
    }
    $b32 = @{
        SHA1   = 'GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ'
        SHA256 = 'GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQGEZA'
        SHA512 = ('GEZDGNBVGY3TQOJQ' * 6) + 'GEZDGNA'
    }
    foreach ($alg in 'SHA1', 'SHA256', 'SHA512') {
        & $check ($ascii.GetString((ConvertFrom-Base32 $b32[$alg])) -ceq $seed[$alg]) "base32 $alg seed"
    }

    # RFC 6238 Appendix B: 8 digits, period 30
    $vectors = @(
        @(59, '94287082', '46119246', '90693936'),
        @(1111111109, '07081804', '68084774', '25091201'),
        @(1111111111, '14050471', '67062674', '99943326'),
        @(1234567890, '89005924', '91819424', '93441116'),
        @(2000000000, '69279037', '90698825', '38618901'),
        @(20000000000, '65353130', '77737706', '47863826')
    )
    foreach ($v in $vectors) {
        $i = 1
        foreach ($alg in 'SHA1', 'SHA256', 'SHA512') {
            $got = Get-TotpCode -Key ($ascii.GetBytes($seed[$alg])) -UnixTime $v[0] -Period 30 -Digits 8 -Algorithm $alg
            & $check ($got -ceq $v[$i]) "RFC 6238 $alg t=$($v[0]) got $got want $($v[$i])"
            $i++
        }
    }

    # RFC 4226 Appendix D: HOTP SHA1, 6 digits; counter c is time c*30
    $hotp = '755224', '287082', '359152', '969429', '338314', '254676', '287922', '162583', '399871', '520489'
    for ($c = 0; $c -lt 10; $c++) {
        $got = Get-TotpCode -Key ($ascii.GetBytes($seed.SHA1)) -UnixTime ($c * 30) -Period 30 -Digits 6 -Algorithm SHA1
        & $check ($got -ceq $hotp[$c]) "RFC 4226 counter $c got $got want $($hotp[$c])"
    }

    $k1 = ConvertFrom-Base32 'GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQGEZA===='
    $k2 = ConvertFrom-Base32 'gezd gnbv gy3t qojq gezd gnbv gy3t qojq gezd gnbv gy3t qojq geza'
    & $check (($ascii.GetString($k1) -ceq $seed.SHA256) -and ($ascii.GetString($k2) -ceq $seed.SHA256)) 'base32 padding, spaces and lower case'
    foreach ($bad in 'GEZDGNB1', 'G', 'GEZDGNBVG') {
        $msg = $null
        try { [void](ConvertFrom-Base32 $bad) } catch { $msg = $_.Exception.Message }
        & $check ($null -ne $msg -and -not $msg.Contains($bad)) "base32 rejects '$bad' without quoting it"
    }

    $good = @(
        @('otpauth://totp/Certum:user%40example.com?secret=GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQGEZA&issuer=Certum&algorithm=SHA256&digits=6&period=30', '', 'SHA256/uri/6/30'),
        @('otpauth://totp/Certum:user@example.com?secret=GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ&issuer=Certum', '', 'SHA1/default/6/30'),
        @('otpauth://totp/x?secret=GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ', 'sha-256', 'SHA256/variable/6/30'),
        @('OTPAUTH://TOTP/x?Secret=gezdgnbvgy3tqojq&Algorithm=sha-512&Digits=8&Period=60', '', 'SHA512/uri/8/60'),
        @('otpauth://totp/x?secret=GEZDGNBVGY3TQOJQ&algorithm=SHA256', 'SHA256', 'SHA256/uri/6/30')
    )
    foreach ($g in $good) {
        try {
            $o = ConvertFrom-OtpAuthUri -Uri $g[0] -DefaultAlgorithm $g[1]
            $got = '{0}/{1}/{2}/{3}' -f $o.Algorithm, $o.AlgorithmSource, $o.Digits, $o.Period
        } catch { $got = $_.Exception.Message }
        & $check ($got -eq $g[2]) "otpauth parse, want $($g[2]), got $got"
    }
    $bad = @(
        @('otpauth://hotp/x?secret=GEZDGNBVGY3TQOJQ&counter=1', ''),
        @('https://example.com/?secret=GEZDGNBVGY3TQOJQ', ''),
        @('otpauth://totp/x?issuer=Certum', ''),
        @('otpauth://totp/x?secret=GEZDGNBVGY3TQOJQ&algorithm=MD5', ''),
        @('otpauth://totp/x?secret=GEZDGNBVGY3TQOJQ&digits=5', ''),
        @('otpauth://totp/x?secret=GEZDGNBVGY3TQOJQ&period=10', ''),
        @('otpauth://totp/x?secret=GEZDGNBVGY3TQOJQ&algorithm=SHA256', 'SHA1')
    )
    foreach ($b in $bad) {
        $threw = $false
        try { [void](ConvertFrom-OtpAuthUri -Uri $b[0] -DefaultAlgorithm $b[1]) } catch { $threw = $true }
        & $check $threw "otpauth rejects $($b[0]) with '$($b[1])'"
    }

    # the whole path, URI to code
    $got = Get-Totp -NoMask -Uri ('otpauth://totp/Test:user%40example.com?secret=' + $b32.SHA1 + '&issuer=Test&digits=8') -DefaultAlgorithm '' -UnixTime 59
    & $check ($got -ceq '94287082') 'Get-Totp SHA1 by default'
    $got = Get-Totp -NoMask -Uri ('otpauth://totp/x?algorithm=SHA512&digits=8&secret=' + $b32.SHA512) -DefaultAlgorithm '' -UnixTime 1111111111
    & $check ($got -ceq '99943326') 'Get-Totp SHA512 from the URI'
    $got = Get-Totp -NoMask -Uri ('otpauth://totp/x?secret=' + $b32.SHA256) -DefaultAlgorithm 'SHA256' -UnixTime 1234567890
    & $check ($got -ceq '819424') 'Get-Totp SHA256 from the variable, 6 digits'

    # log redaction (Protect-SsdText), with made-up values
    $red = @(
        @('login of Someone.Else@Example.com failed', 'someone.else@example.com', 'login of <user> failed'),
        @('user irmak@EXAMPLE.com here', 'Irmak@example.com', 'user <user> here'),
        @('GET /api?user=a.b%40example.org&x=1', '', 'GET /api?user=<email>&x=1'),
        @('session eyJhbGciOiJIUzI1NiJ9.e30 ok', '', 'session <long> ok'),
        @('card 1234567890123 found', '', 'card <n> found'),
        @('Invalid user name or token', '', 'Invalid user name or token')
    )
    foreach ($x in $red) {
        $got = Protect-SsdText $x[0] $x[1]
        & $check ($got -ceq $x[2]) "Protect-SsdText want '$($x[2])' got '$got'"
    }

    Write-Host ('totp self-test: {0} of {1} passed (PowerShell {2})' -f $r.Pass, ($r.Pass + $r.Fail), $PSVersionTable.PSVersion)
    foreach ($f in $failed) { Write-Host "  FAIL $f" }
    return ($r.Fail -eq 0)
}

function Show-TotpCheck {
    # Local console only: shows the current SHA1 and SHA256 codes for your URI so
    # you can see which one the SimplySign mobile app shows. No network, stores nothing.
    if ($env:GITHUB_ACTIONS -eq 'true') { throw 'Show-TotpCheck is for a local console only.' }
    $secure = Read-Host -AsSecureString -Prompt 'Paste the otpauth:// URI (it is not shown)'
    $bstr = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($secure)
    try { $uri = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($bstr) } finally { [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($bstr) }
    $otp = ConvertFrom-OtpAuthUri -Uri $uri -DefaultAlgorithm ''
    $uri = $null
    try {
        if ($otp.AlgorithmSource -eq 'uri') {
            Write-Host "The URI has algorithm=$($otp.Algorithm), so CERTUM_TOTP_ALGORITHM is not needed."
            return
        }
        $now = [DateTimeOffset]::UtcNow.ToUnixTimeSeconds()
        foreach ($alg in 'SHA1', 'SHA256') {
            Write-Host ('{0,-7} {1}' -f $alg, (Get-TotpCode -Key $otp.Key -UnixTime $now -Period $otp.Period -Digits $otp.Digits -Algorithm $alg))
        }
        Write-Host ('{0} s left. Set CERTUM_TOTP_ALGORITHM to the line that matches the phone.' -f ($otp.Period - ($now % $otp.Period)))
    } finally {
        [Array]::Clear($otp.Key, 0, $otp.Key.Length)
    }
}

# ---------------------------------------------------------------- install

function Assert-AssecoSignature {
    param([Parameter(Mandatory = $true)][string]$Path)
    $name = Split-Path -Leaf $Path
    $sig = Get-AuthenticodeSignature -LiteralPath $Path
    $leaf = $sig.SignerCertificate
    if ($sig.Status -ne 'Valid' -or $null -eq $leaf) { throw "Authenticode of $name is $($sig.Status), expected Valid." }
    # compare simple names only: the full subject has non-ASCII text and the
    # signer certificate changes between Asseco files
    $signer = $leaf.GetNameInfo([Security.Cryptography.X509Certificates.X509NameType]::SimpleName, $false)
    $issuer = $leaf.GetNameInfo([Security.Cryptography.X509Certificates.X509NameType]::SimpleName, $true)
    if ($signer -ne $SsdConfig.SignerName -or $issuer -ne $SsdConfig.IssuerName) {
        throw "$name is signed by '$signer' (issuer '$issuer'), expected '$($SsdConfig.SignerName)' (issuer '$($SsdConfig.IssuerName)')."
    }
    Write-Host "${name}: Authenticode Valid, signer $signer, issuer $issuer"
}

function Install-SimplySign {
    # Download the pinned MSI, check size, Authenticode signer and the optional
    # SHA-256 pin, then install it silently for all users.
    [CmdletBinding()]
    param([string]$Sha256 = $env:SIMPLYSIGN_MSI_SHA256)
    Set-StrictMode -Version 2
    $ErrorActionPreference = 'Stop'
    $ProgressPreference = 'SilentlyContinue'
    $total = [Diagnostics.Stopwatch]::StartNew()
    $tmp = $env:RUNNER_TEMP
    if (-not $tmp) { $tmp = [IO.Path]::GetTempPath() }
    $msi = Join-Path $tmp 'SimplySignDesktop.msi'
    $log = Join-Path $tmp 'simplysign-msi.log'

    Write-Host "downloading $($SsdConfig.MsiUrl)"
    $sw = [Diagnostics.Stopwatch]::StartNew()
    for ($i = 1; ; $i++) {
        try {
            Invoke-WebRequest -Uri $SsdConfig.MsiUrl -OutFile $msi -UseBasicParsing -TimeoutSec 600
            break
        } catch {
            if ($i -ge 3) { throw }
            Write-Host "::warning::download try $i failed: $($_.Exception.Message)"
            Start-Sleep -Seconds 5
        }
    }
    $size = (Get-Item -LiteralPath $msi).Length
    Write-Host ('downloaded {0} bytes in {1:N0} s' -f $size, $sw.Elapsed.TotalSeconds)
    if ($size -ne $SsdConfig.MsiSize) { throw "The MSI has $size bytes, expected $($SsdConfig.MsiSize)." }
    Assert-AssecoSignature -Path $msi
    $hash = (Get-FileHash -LiteralPath $msi -Algorithm SHA256).Hash
    Write-Host "MSI sha256 $hash"
    if ($Sha256) {
        if ($hash -ne $Sha256.Trim()) { throw "The MSI SHA-256 is $hash, expected $Sha256." }
        Write-Host 'MSI sha256 matches the pin'
    } else {
        Write-Host 'no MSI sha256 pin set (SIMPLYSIGN_MSI_SHA256 in sign.yml); size and signer checks passed'
    }

    $sw.Restart()
    $msiArgs = @('/i', ('"{0}"' -f $msi), '/qn', '/norestart', 'ALLUSERS=1', 'REBOOT=ReallySuppress', '/l*v', ('"{0}"' -f $log))
    $p = Start-Process -FilePath 'msiexec.exe' -ArgumentList $msiArgs -Wait -PassThru
    $code = $p.ExitCode
    Write-Host ('msiexec exit code {0} after {1:N0} s' -f $code, $sw.Elapsed.TotalSeconds)
    if ($code -eq 3010) {
        Write-Host '::warning::msiexec asks for a reboot (3010); going on without one'
    } elseif ($code -ne 0) {
        if (Test-Path -LiteralPath $log) { Get-Content -LiteralPath $log -Tail 40 | ForEach-Object { Write-Host $_ } }
        throw "msiexec failed with exit code $code."
    }
    $exe = $SsdConfig.Exe
    if (-not (Test-Path -LiteralPath $exe)) { throw "SimplySign Desktop is not at $exe after the install." }
    Assert-AssecoSignature -Path $exe
    Write-Host ('SimplySignDesktop.exe file version {0}' -f (Get-Item -LiteralPath $exe).VersionInfo.FileVersion)
    Write-Host ('Install-SimplySign took {0:N0} s' -f $total.Elapsed.TotalSeconds)
}

# ---------------------------------------------------------------- windows

function Initialize-SsdWin32 {
    # compiles the small user32 helper once per session; touches no window
    if (-not ('SsdWin32' -as [type])) {
        Add-Type -TypeDefinition @'
using System;
using System.Collections.Generic;
using System.Runtime.InteropServices;
using System.Text;

public static class SsdWin32
{
    public delegate bool EnumProc(IntPtr hWnd, IntPtr lParam);

    [StructLayout(LayoutKind.Sequential)]
    public struct RECT { public int Left, Top, Right, Bottom; }

    [StructLayout(LayoutKind.Sequential)]
    public struct GUITHREADINFO
    {
        public int cbSize; public int flags;
        public IntPtr hwndActive, hwndFocus, hwndCapture, hwndMenuOwner, hwndMoveSize, hwndCaret;
        public RECT rcCaret;
    }

    const uint WM_GETTEXT = 0x000D, WM_GETTEXTLENGTH = 0x000E, WM_COMMAND = 0x0111, BM_CLICK = 0x00F5;
    const uint SMTO_ABORTIFHUNG = 0x0002, KEYEVENTF_KEYUP = 0x0002;
    const int GWL_STYLE = -16, ES_PASSWORD = 0x0020, MSGBOX_TEXT_ID = 0xFFFF;
    const byte VK_MENU = 0x12;

    [DllImport("user32.dll")] static extern bool EnumWindows(EnumProc cb, IntPtr lParam);
    [DllImport("user32.dll")] static extern bool EnumChildWindows(IntPtr parent, EnumProc cb, IntPtr lParam);
    [DllImport("user32.dll")] static extern uint GetWindowThreadProcessId(IntPtr hWnd, out uint pid);
    [DllImport("user32.dll")] public static extern bool IsWindowVisible(IntPtr hWnd);
    [DllImport("user32.dll")] public static extern bool IsWindow(IntPtr hWnd);
    [DllImport("user32.dll", CharSet = CharSet.Unicode)] static extern int GetClassNameW(IntPtr hWnd, StringBuilder sb, int max);
    [DllImport("user32.dll", CharSet = CharSet.Unicode)] static extern int GetWindowTextW(IntPtr hWnd, StringBuilder sb, int max);
    [DllImport("user32.dll")] static extern bool GetWindowRect(IntPtr hWnd, out RECT r);
    [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr hWnd);
    [DllImport("user32.dll")] public static extern IntPtr GetForegroundWindow();
    [DllImport("user32.dll")] public static extern bool ShowWindow(IntPtr hWnd, int cmd);
    [DllImport("user32.dll")] static extern bool GetGUIThreadInfo(uint threadId, ref GUITHREADINFO info);
    [DllImport("user32.dll")] static extern IntPtr GetDlgItem(IntPtr dialog, int id);
    [DllImport("user32.dll", EntryPoint = "GetWindowLongW")] static extern int GetWindowLong(IntPtr hWnd, int index);
    [DllImport("user32.dll")] static extern bool PostMessageW(IntPtr hWnd, uint msg, IntPtr w, IntPtr l);
    [DllImport("user32.dll")] static extern void keybd_event(byte vk, byte scan, uint flags, UIntPtr extra);
    [DllImport("user32.dll", EntryPoint = "SendMessageTimeoutW")]
    static extern IntPtr SendMsg(IntPtr hWnd, uint msg, IntPtr w, IntPtr l, uint flags, uint timeout, out IntPtr result);
    [DllImport("user32.dll", EntryPoint = "SendMessageTimeoutW", CharSet = CharSet.Unicode)]
    static extern IntPtr SendMsgText(IntPtr hWnd, uint msg, IntPtr w, StringBuilder l, uint flags, uint timeout, out IntPtr result);

    public static uint ProcessOf(IntPtr h) { uint pid; GetWindowThreadProcessId(h, out pid); return pid; }

    // visible top-level windows of the given processes
    public static IntPtr[] TopLevel(int[] pids)
    {
        var list = new List<IntPtr>();
        EnumWindows(delegate(IntPtr h, IntPtr l) {
            if (IsWindowVisible(h) && Array.IndexOf(pids, (int)ProcessOf(h)) >= 0) list.Add(h);
            return true;
        }, IntPtr.Zero);
        return list.ToArray();
    }

    // visible descendants of a window, any depth
    public static IntPtr[] Children(IntPtr parent)
    {
        var list = new List<IntPtr>();
        EnumChildWindows(parent, delegate(IntPtr h, IntPtr l) {
            if (IsWindowVisible(h)) list.Add(h);
            return true;
        }, IntPtr.Zero);
        return list.ToArray();
    }

    public static string ClassOf(IntPtr h) { var sb = new StringBuilder(256); GetClassNameW(h, sb, sb.Capacity); return sb.ToString(); }
    public static string TitleOf(IntPtr h) { var sb = new StringBuilder(512); GetWindowTextW(h, sb, sb.Capacity); return sb.ToString(); }
    public static int[] RectOf(IntPtr h) { RECT r; GetWindowRect(h, out r); return new int[] { r.Left, r.Top, r.Right, r.Bottom }; }
    public static bool IsPassword(IntPtr edit) { return (GetWindowLong(edit, GWL_STYLE) & ES_PASSWORD) != 0; }

    // -1 when the window does not answer
    public static int TextLength(IntPtr h)
    {
        IntPtr len;
        if (SendMsg(h, WM_GETTEXTLENGTH, IntPtr.Zero, IntPtr.Zero, SMTO_ABORTIFHUNG, 2000, out len) == IntPtr.Zero) return -1;
        return len.ToInt32();
    }

    // text of a control in another process; never log the text of an EDIT
    public static string TextOf(IntPtr h)
    {
        int n = TextLength(h);
        if (n < 0) return null;
        var sb = new StringBuilder(n + 2);
        IntPtr copied;
        if (SendMsgText(h, WM_GETTEXT, (IntPtr)sb.Capacity, sb, SMTO_ABORTIFHUNG, 2000, out copied) == IntPtr.Zero) return null;
        return sb.ToString();
    }

    // the control with keyboard focus in the GUI thread that owns this window
    public static IntPtr FocusOf(IntPtr h)
    {
        uint pid;
        uint tid = GetWindowThreadProcessId(h, out pid);
        var info = new GUITHREADINFO();
        info.cbSize = Marshal.SizeOf(typeof(GUITHREADINFO));
        return GetGUIThreadInfo(tid, ref info) ? info.hwndFocus : IntPtr.Zero;
    }

    // message box text (static control 0xFFFF), null if there is none
    public static string MessageText(IntPtr dialog)
    {
        IntPtr t = GetDlgItem(dialog, MSGBOX_TEXT_ID);
        return t == IntPtr.Zero ? null : TextOf(t);
    }

    // true if the dialog has a control with this id (IDNO = 7 for a No button)
    public static bool HasItem(IntPtr dialog, int id) { return GetDlgItem(dialog, id) != IntPtr.Zero; }

    // press a dialog button by id: WM_COMMAND to the dialog, or BM_CLICK to the button
    public static bool PressButton(IntPtr dialog, int id, bool click)
    {
        IntPtr b = GetDlgItem(dialog, id);
        if (b == IntPtr.Zero) return false;
        if (click) return PostMessageW(b, BM_CLICK, IntPtr.Zero, IntPtr.Zero);
        return PostMessageW(dialog, WM_COMMAND, (IntPtr)id, b);
    }

    // an Alt tap lets this process move the foreground window
    public static void TapAlt()
    {
        keybd_event(VK_MENU, 0, 0, UIntPtr.Zero);
        keybd_event(VK_MENU, 0, KEYEVENTF_KEYUP, UIntPtr.Zero);
    }
}
'@
    }
    Add-Type -AssemblyName System.Windows.Forms
}

function Get-SsdProcess {
    @(Get-Process -Name $SsdConfig.ProcessName -ErrorAction SilentlyContinue)
}

function Stop-SsdProcess {
    param([int]$GraceSeconds = 0)
    if (@(Get-SsdProcess).Count -eq 0) { return }
    if ($GraceSeconds -gt 0) {
        foreach ($p in @(Get-SsdProcess)) { try { [void]$p.CloseMainWindow() } catch { } }
        $deadline = [DateTime]::UtcNow.AddSeconds($GraceSeconds)
        while (@(Get-SsdProcess).Count -gt 0 -and [DateTime]::UtcNow -lt $deadline) { Start-Sleep -Milliseconds 250 }
    }
    Get-SsdProcess | Stop-Process -Force -ErrorAction SilentlyContinue
}

function Get-SsdWindow {
    # visible top-level windows of all SimplySign Desktop processes
    $ids = @(Get-SsdProcess | ForEach-Object { $_.Id })
    if ($ids.Count -eq 0) { return }
    foreach ($h in [SsdWin32]::TopLevel([int[]]$ids)) {
        [pscustomobject]@{ Handle = $h; Class = [SsdWin32]::ClassOf($h); Title = [SsdWin32]::TitleOf($h) }
    }
}

function Find-SsdLoginDialog {
    # a WinForms window of the app with exactly two visible edit fields:
    # the upper one is the user name, the lower one the token
    foreach ($w in @(Get-SsdWindow)) {
        if ($w.Class -notlike 'WindowsForms10.Window.*') { continue }
        $edits = @([SsdWin32]::Children($w.Handle) | Where-Object { [SsdWin32]::ClassOf($_) -like '*EDIT*' })
        if ($edits.Count -ne 2) { continue }
        $edits = @($edits | Sort-Object { [SsdWin32]::RectOf($_)[1] })
        return [pscustomobject]@{ Handle = $w.Handle; User = $edits[0]; Token = $edits[1] }
    }
    return $null
}

function Get-SsdModal {
    # message boxes (#32770) of the app, with their text
    foreach ($w in @(Get-SsdWindow)) {
        if ($w.Class -ne '#32770') { continue }
        [pscustomobject]@{
            Handle = $w.Handle
            Title  = $w.Title
            Text   = [string][SsdWin32]::MessageText($w.Handle)
            Rect   = ([SsdWin32]::RectOf($w.Handle) -join ',')
        }
    }
}

function Get-SsdModalKind {
    # 'invalid' for SimplySign's own login rejection (only that box may lead to
    # the retry), 'no' for any other box with a No button, 'unknown' otherwise.
    # The update prompt is a Yes/No box, but its exact text is not published,
    # so it is found by its No button; No never starts a download.
    param($Modal)
    if ($Modal.Text -like '*Invalid user name or token*') { return 'invalid' }
    if ([SsdWin32]::HasItem($Modal.Handle, 7)) { return 'no' }
    return 'unknown'
}

function Close-SsdModal {
    # press the first button id that exists; posted, so a modal loop cannot block us
    param($Modal, [int[]]$Ids)
    foreach ($click in $false, $true) {
        [void][SsdWin32]::SetForegroundWindow($Modal.Handle)
        foreach ($id in $Ids) { if ([SsdWin32]::PressButton($Modal.Handle, $id, $click)) { break } }
        for ($i = 0; $i -lt 20; $i++) {
            Start-Sleep -Milliseconds 250
            if (-not [SsdWin32]::IsWindow($Modal.Handle)) { return }
        }
    }
    throw 'A SimplySign message box did not close.'
}

function Protect-SsdText {
    # for window titles and message box text: hide the user name, e-mail
    # addresses (also URL-encoded), runs of 16 or more token-like characters
    # and long numbers. The app log is never printed, not even through this.
    # Case-sensitive patterns on purpose: -replace ignores case by the current
    # culture, and under tr-TR 'I' then misses [A-Za-z].
    param([string]$Text, [string]$User)
    if ([string]::IsNullOrEmpty($Text)) { return '' }
    $t = $Text
    if ($User) {
        $opt = [Text.RegularExpressions.RegexOptions]::IgnoreCase -bor [Text.RegularExpressions.RegexOptions]::CultureInvariant
        $t = [regex]::Replace($t, [regex]::Escape($User), '<user>', $opt)
    }
    $t = $t -creplace '[^\s@<>]+@[^\s@<>]+', '<email>'
    $t = $t -creplace '[A-Za-z0-9._+-]+%40[A-Za-z0-9.-]+', '<email>'
    $t = $t -creplace '[A-Za-z0-9+/=_.%-]{16,}', '<long>'
    return ($t -creplace '\d{10,}', '<n>')
}

function Set-SsdForeground {
    param([IntPtr]$Handle)
    for ($i = 0; $i -lt 12; $i++) {
        [void][SsdWin32]::ShowWindow($Handle, 9)   # SW_RESTORE
        [void][SsdWin32]::SetForegroundWindow($Handle)
        Start-Sleep -Milliseconds 250
        if ([SsdWin32]::GetForegroundWindow() -eq $Handle) { return $true }
        if ($i -eq 3) { [SsdWin32]::TapAlt() }
    }
    try { [void](New-Object -ComObject WScript.Shell).AppActivate([int][SsdWin32]::ProcessOf($Handle)) } catch { }
    Start-Sleep -Milliseconds 500
    return ([SsdWin32]::GetForegroundWindow() -eq $Handle)
}

function Send-SsdKeys {
    # keys go only to the login window: check the foreground before every sequence
    param($Dialog, [string]$Keys)
    if ([SsdWin32]::GetForegroundWindow() -ne $Dialog.Handle) {
        if (-not (Set-SsdForeground $Dialog.Handle)) { throw 'The login window lost the foreground.' }
    }
    [System.Windows.Forms.SendKeys]::SendWait($Keys)
}

function ConvertTo-SendKeysText {
    param([string]$Text)
    $sb = New-Object System.Text.StringBuilder
    foreach ($ch in $Text.ToCharArray()) {
        if ('+^%~(){}[]'.IndexOf($ch) -ge 0) { [void]$sb.Append('{').Append($ch).Append('}') } else { [void]$sb.Append($ch) }
    }
    $sb.ToString()
}

function Clear-SsdClipboard {
    try { Set-Clipboard -Value ' ' } catch { Write-Host "::warning::could not clear the clipboard: $($_.Exception.Message)" }
}

function Move-SsdFocus {
    param($Dialog, [IntPtr]$Target)
    for ($i = 0; $i -le 8; $i++) {
        if ([SsdWin32]::FocusOf($Dialog.Handle) -eq $Target) { return }
        Send-SsdKeys $Dialog '{TAB}'
        Start-Sleep -Milliseconds 200
    }
    throw 'Could not move the keyboard focus to a login field.'
}

function Set-SsdField {
    # clipboard paste (atomic); -Type types with SendKeys instead
    param($Dialog, [IntPtr]$Edit, [string]$Text, [switch]$Type)
    Move-SsdFocus $Dialog $Edit
    Send-SsdKeys $Dialog '^a'
    Send-SsdKeys $Dialog '{DEL}'
    if ($Type) {
        Send-SsdKeys $Dialog (ConvertTo-SendKeysText $Text)
    } else {
        Set-Clipboard -Value $Text
        try {
            Send-SsdKeys $Dialog '^v'
            Start-Sleep -Milliseconds 250
        } finally {
            Clear-SsdClipboard
        }
    }
    Start-Sleep -Milliseconds 250
}

function Test-SsdUserField {
    param($Dialog, [string]$User)
    return ([string][SsdWin32]::TextOf($Dialog.User) -ceq $User)
}

function Test-SsdTokenField {
    # The code must read back exactly. A WinForms TextBox with
    # UseSystemPasswordChar or PasswordChar still answers WM_GETTEXT from
    # another process of the same user (measured on Windows 11 with a probe
    # form), so the text is normally readable. Only if the field hides its
    # text (no answer, or empty text with a length above 0) is a matching
    # length enough, and only for a pasted code: typed keys can arrive
    # reordered. An empty field never counts as filled.
    param($Dialog, [string]$Code, [switch]$Pasted)
    $text = [SsdWin32]::TextOf($Dialog.Token)
    if ($null -ne $text -and $text -ceq $Code) { return $true }
    $n = [SsdWin32]::TextLength($Dialog.Token)
    $hidden = ($n -gt 0) -and ($null -eq $text -or $text.Length -eq 0)
    if ($hidden -and $Pasted -and $n -eq $Code.Length) {
        Write-Host 'token field hides its text; its length matches the pasted code'
        return $true
    }
    return $false
}

function Wait-SsdTotpWindow {
    # wait for a time step newer than AfterStep with at least MinLeft seconds to go
    param([int]$Period, [long]$AfterStep, [int]$MinLeft)
    while ($true) {
        $ms = [DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds()
        $step = [long][Math]::Floor($ms / ($Period * 1000.0))
        $leftMs = ($Period * 1000) - ($ms % ($Period * 1000))
        if ($step -gt $AfterStep -and $leftMs -ge ($MinLeft * 1000)) { return }
        Start-Sleep -Milliseconds ([int]$leftMs + 300)
    }
}

function Submit-SsdLogin {
    # enter the user name and a fresh code, confirm both, then press Enter.
    # Nothing is submitted unless both fields read back as expected (see
    # Test-SsdTokenField for the one case where a length has to do).
    param($Dialog, [string]$User, $Otp, [hashtable]$State)
    if (-not (Test-SsdUserField $Dialog $User)) {
        Set-SsdField $Dialog $Dialog.User $User
        if (-not (Test-SsdUserField $Dialog $User)) {
            Write-Host 'user name did not read back after the paste; typing it'
            Set-SsdField $Dialog $Dialog.User $User -Type
            if (-not (Test-SsdUserField $Dialog $User)) { throw 'The user name field does not hold the user name after two tries.' }
        }
    }
    Write-Host 'user name entered and confirmed'

    $ok = $false
    $step = [long]-1
    for ($try = 1; $try -le 2 -and -not $ok; $try++) {
        Move-SsdFocus $Dialog $Dialog.Token
        # compute the code late, with at least MinSecondsLeft in its time step
        Wait-SsdTotpWindow -Period $Otp.Period -AfterStep $State.LastStep -MinLeft $SsdConfig.MinSecondsLeft
        $now = [DateTimeOffset]::UtcNow.ToUnixTimeSeconds()
        $step = [long][Math]::Floor($now / $Otp.Period)
        $code = Get-TotpCode -Key $Otp.Key -UnixTime $now -Period $Otp.Period -Digits $Otp.Digits -Algorithm $Otp.Algorithm
        Add-ActionsMask $code
        Set-SsdField $Dialog $Dialog.Token $code -Type:($try -eq 2)
        $ok = (Test-SsdTokenField $Dialog $code -Pasted:($try -eq 1)) -and (Test-SsdUserField $Dialog $User)
        if (-not $ok) {
            Write-Host "the login fields did not read back as expected (try $try)"
            if (-not (Test-SsdUserField $Dialog $User)) { Set-SsdField $Dialog $Dialog.User $User -Type }
        }
    }
    if (-not $ok) { throw 'The login fields did not hold the expected values after two tries; Enter was not pressed.' }

    Move-SsdFocus $Dialog $Dialog.Token
    $left = $Otp.Period - ([DateTimeOffset]::UtcNow.ToUnixTimeSeconds() % $Otp.Period)
    Send-SsdKeys $Dialog '{ENTER}'
    $State.LastStep = $step
    $State.Submits++
    $code = $null
    Write-Host "code submitted: time step $step, $left s left in it"
}

function Get-SsdCertificate {
    # the certificate in CurrentUser\My, only if it has a usable private key
    param([string]$Thumbprint)
    $storeName = [System.Security.Cryptography.X509Certificates.StoreName]::My
    $storeLocation = [System.Security.Cryptography.X509Certificates.StoreLocation]::CurrentUser
    $store = New-Object System.Security.Cryptography.X509Certificates.X509Store -ArgumentList $storeName, $storeLocation
    try {
        $store.Open([System.Security.Cryptography.X509Certificates.OpenFlags]::ReadOnly)
        foreach ($c in $store.Certificates) {
            if ($c.Thumbprint -eq $Thumbprint -and $c.HasPrivateKey -and $c.NotAfter -gt [DateTime]::Now) { return $c }
        }
    } finally {
        $store.Close()
    }
    return $null
}

function Wait-SsdLogin {
    # 'ok' when the certificate shows up, 'rejected' on SimplySign's own
    # rejection message; anything else throws
    param([string]$Thumbprint, [int]$Seconds, [string]$User)
    $deadline = [DateTime]::UtcNow.AddSeconds($Seconds)
    while ([DateTime]::UtcNow -lt $deadline) {
        Start-Sleep -Seconds 1
        if (Get-SsdCertificate $Thumbprint) { return 'ok' }
        foreach ($m in @(Get-SsdModal)) {
            $kind = Get-SsdModalKind $m
            if ($kind -eq 'invalid') {
                Write-Host 'SimplySign says: Invalid user name or token'
                Close-SsdModal $m @(1, 2)
                return 'rejected'
            }
            if ($kind -eq 'no') {
                Write-Host ("SimplySign asks (title '{0}'): {1}; answering No" -f (Protect-SsdText $m.Title $User), (Protect-SsdText $m.Text $User))
                Close-SsdModal $m @(7)
                continue
            }
            throw ("SimplySign showed an unexpected message (title '{0}', rect {1}): {2}" -f (Protect-SsdText $m.Title $User), $m.Rect, (Protect-SsdText $m.Text $User))
        }
        if (@(Get-SsdProcess).Count -eq 0) { throw 'SimplySign Desktop exited during the login.' }
    }
    throw "No certificate $Thumbprint with a private key after $Seconds s, and no message from SimplySign."
}

function Open-SsdLoginDialog {
    # start the app, find its login window, clear an update prompt, focus it
    param([string]$Exe, [string]$User)
    $dialog = $null
    for ($launch = 1; $launch -le 2 -and $null -eq $dialog; $launch++) {
        if ($launch -eq 2) {
            if (@(Get-SsdProcess).Count -eq 0) { break }
            # the running instance shows its login window when started again
            Write-Host 'no login window after 30 s; starting SimplySign Desktop once more'
        }
        $sw = [Diagnostics.Stopwatch]::StartNew()
        Start-Process -FilePath $Exe
        while ($sw.Elapsed.TotalSeconds -lt 30) {
            Start-Sleep -Milliseconds 500
            $dialog = Find-SsdLoginDialog
            if ($dialog) {
                Write-Host ('login window after {0:N1} s (launch {1})' -f $sw.Elapsed.TotalSeconds, $launch)
                break
            }
        }
    }
    if (-not $dialog) { throw 'The SimplySign login window did not appear.' }

    for ($i = 0; $i -lt 5; $i++) {
        foreach ($m in @(Get-SsdModal)) {
            if ((Get-SsdModalKind $m) -eq 'no') {
                Write-Host ("SimplySign asks (title '{0}'): {1}; answering No" -f (Protect-SsdText $m.Title $User), (Protect-SsdText $m.Text $User))
                Close-SsdModal $m @(7)
            } else {
                throw ('SimplySign showed an unexpected message before the login: {0}' -f (Protect-SsdText $m.Text $User))
            }
        }
        Start-Sleep -Seconds 1
    }
    $dialog = Find-SsdLoginDialog
    if (-not $dialog) { throw 'The SimplySign login window closed before the login.' }
    if (-not (Set-SsdForeground $dialog.Handle)) { throw 'The login window could not be brought to the foreground.' }
    Write-Host ('login window found: class {0}, token field password style: {1}' -f [SsdWin32]::ClassOf($dialog.Handle), [SsdWin32]::IsPassword($dialog.Token))
    return $dialog
}

function Write-SsdDiagnostics {
    # safe to print: process and window facts, message box text with private
    # parts hidden, and counts of known markers in the app's own log. Never
    # the log lines themselves: their format is not published, they may hold
    # session data, and the Actions log of this repo is public.
    param([string]$User)
    Write-Host '--- SimplySign diagnostics ---'
    try {
        foreach ($p in @(Get-SsdProcess)) {
            Write-Host ('process {0}, started {1:u}, responding {2}' -f $p.Id, $p.StartTime, $p.Responding)
        }
        if ('SsdWin32' -as [type]) {
            foreach ($w in @(Get-SsdWindow)) {
                $kids = @([SsdWin32]::Children($w.Handle))
                $edits = @($kids | Where-Object { [SsdWin32]::ClassOf($_) -like '*EDIT*' })
                $buttons = @($kids | Where-Object { [SsdWin32]::ClassOf($_) -like '*BUTTON*' })
                $focused = [Array]::IndexOf([object[]]$edits, [SsdWin32]::FocusOf($w.Handle))
                Write-Host ("window {0}: class {1}, title '{2}', rect {3}, {4} edit, {5} button, focused edit {6}" -f $w.Handle, $w.Class, (Protect-SsdText $w.Title $User), ([SsdWin32]::RectOf($w.Handle) -join ','), $edits.Count, $buttons.Count, $focused)
                if ($w.Class -eq '#32770') { Write-Host ('  message: {0}' -f (Protect-SsdText ([SsdWin32]::MessageText($w.Handle)) $User)) }
            }
        }
    } catch {
        Write-Host "diagnostics: $($_.Exception.Message)"
    }
    try {
        $dir = Join-Path ([Environment]::GetFolderPath('MyDocuments')) 'SimplySignLog'
        $log = Get-ChildItem -LiteralPath $dir -Filter 'SimplySign_*_log.txt' -ErrorAction SilentlyContinue | Sort-Object LastWriteTime -Descending | Select-Object -First 1
        if ($log) {
            $lines = @(Get-Content -LiteralPath $log.FullName)
            Write-Host ('app log: {0} lines, last written {1:u}; lines that contain:' -f $lines.Count, $log.LastWriteTimeUtc)
            $markers = [ordered]@{
                'LoginForm'           = 'LoginForm'
                'logging in'          = 'logging in'
                'login ok'            = 'login ok'
                'login failed'        = 'login failed'
                'token valid for'     = 'token valid for'
                'listCards ok'        = 'listCards ok'
                'listCertificates ok' = 'listCertificates.* ok'
                'error or exception'  = 'error|exception'
            }
            foreach ($name in $markers.Keys) {
                $n = @($lines | Where-Object { $_ -match $markers[$name] }).Count
                Write-Host ("  '{0}': {1}" -f $name, $n)
            }
        } else {
            Write-Host 'no SimplySign app log found'
        }
    } catch {
        Write-Host "app log: $($_.Exception.Message)"
    }
}

function Test-SsdClock {
    # a wrong clock makes a wrong code; compare with the Certum file server
    try {
        $r = Invoke-WebRequest -Uri $SsdConfig.MsiUrl -Method Head -UseBasicParsing -TimeoutSec 30
        $date = [string](@($r.Headers['Date'])[0])
        $server = [DateTimeOffset]::Parse($date, [Globalization.CultureInfo]::InvariantCulture)
    } catch {
        Write-Host "::warning::clock check skipped: $($_.Exception.Message)"
        return
    }
    $skew = ([DateTimeOffset]::UtcNow - $server).TotalSeconds
    Write-Host ('clock: runner minus files.certum.eu = {0} s' -f [Math]::Round($skew, 1))
    if ([Math]::Abs($skew) -gt 10) { throw ('The runner clock is {0} s off, so a TOTP code would be wrong.' -f [Math]::Round($skew)) }
}

# ---------------------------------------------------------------- login

function Connect-SimplySign {
    # Log in to SimplySign Desktop with CERTUM_USERNAME and a TOTP code from
    # CERTUM_OTP_URI, then wait for the certificate with its private key.
    # No code at all unless the TOTP hash is known (algorithm= in the URI or
    # CERTUM_TOTP_ALGORITHM). At most two codes per run: a retry only after
    # SimplySign says the first was invalid, and only with a code from a
    # later time step.
    [CmdletBinding()]
    param([string]$Thumbprint = $env:CERT_SHA1, [int]$WaitSeconds = 90)
    Set-StrictMode -Version 2
    $ErrorActionPreference = 'Stop'
    $ProgressPreference = 'SilentlyContinue'

    # take the secrets out of the environment before any child process starts
    $user = ([string]$env:CERTUM_USERNAME).Trim()
    $uri = [string]$env:CERTUM_OTP_URI
    Remove-Item -Path Env:CERTUM_USERNAME, Env:CERTUM_OTP_URI -ErrorAction SilentlyContinue
    if (-not $user -or -not $uri.Trim()) {
        throw 'CERTUM_USERNAME or CERTUM_OTP_URI is empty: the certum-signing environment lacks the secret, or this branch or tag may not use it.'
    }
    Add-ActionsMask $user
    Add-ActionsMask $user.ToLowerInvariant()
    Add-ActionsMask $user.ToUpperInvariant()

    $thumb = ([string]$Thumbprint -replace '[^0-9A-Fa-f]', '').ToUpperInvariant()
    if ($thumb.Length -ne 40) { throw 'Give the certificate SHA-1 thumbprint (40 hex digits) with -Thumbprint or CERT_SHA1.' }

    if (-not (Test-Totp)) { throw 'The TOTP self-test failed; not logging in.' }
    $otp = ConvertFrom-OtpAuthUri -Uri $uri -DefaultAlgorithm $env:CERTUM_TOTP_ALGORITHM
    $uri = $null
    if ($otp.AlgorithmSource -eq 'default') {
        # The Key URI Format default is SHA1, but the public Certum automations
        # use SHA-256 (jay0lee/certum-cloud-code-sign, dismine). A wrong guess
        # costs two bad logins, so stop before the app even starts.
        [Array]::Clear($otp.Key, 0, $otp.Key.Length)
        throw ('The OTP URI has no algorithm= and the certum-signing environment has no variable CERTUM_TOTP_ALGORITHM, ' +
            'so the TOTP hash is unknown. Find it (Show-TotpCheck in .github/scripts/simplysign.ps1, or the TOTP field ' +
            'of a password manager, against the SimplySign mobile app), set CERTUM_TOTP_ALGORITHM to SHA256 or SHA1, ' +
            'then re-run. No code was sent, so a re-run is safe.')
    }
    Write-Host ('totp: algorithm={0} ({1}) digits={2} period={3}' -f $otp.Algorithm, $otp.AlgorithmSource, $otp.Digits, $otp.Period)

    $state = @{ Submits = 0; LastStep = [long]-1 }
    try {
        Test-SsdClock
        $exe = $SsdConfig.Exe
        if (-not (Test-Path -LiteralPath $exe)) { throw "SimplySign Desktop is not installed at $exe; run Install-SimplySign first." }
        Initialize-SsdWin32
        Stop-SsdProcess
        Set-SsdRegistry
        $dialog = Open-SsdLoginDialog -Exe $exe -User $user
        Submit-SsdLogin -Dialog $dialog -User $user -Otp $otp -State $state
        $result = Wait-SsdLogin -Thumbprint $thumb -Seconds $WaitSeconds -User $user
        if ($result -eq 'rejected') {
            Write-Host 'the first code was rejected; one retry with a code from a later time step'
            $dialog = Find-SsdLoginDialog
            if (-not $dialog) { throw 'The login window is gone after the rejection.' }
            if (-not (Set-SsdForeground $dialog.Handle)) { throw 'The login window could not be brought to the foreground.' }
            Submit-SsdLogin -Dialog $dialog -User $user -Otp $otp -State $state
            $result = Wait-SsdLogin -Thumbprint $thumb -Seconds $WaitSeconds -User $user
            if ($result -eq 'rejected') {
                throw ('SimplySign rejected codes from two different time steps. Do not re-run right away. ' +
                    'Check that no other job or person logged in with this account in the same minute, ' +
                    'the TOTP algorithm (algorithm= in the URI or CERTUM_TOTP_ALGORITHM), the account e-mail ' +
                    'and the runner clock. Repeated failures can lock the account.')
            }
        }
    } catch {
        $msg = $_.Exception.Message
        Write-SsdDiagnostics -User $user
        if ($state.Submits -eq 0) {
            $msg += ' No code was sent, so a re-run is safe.'
        } else {
            $msg += (' {0} code(s) were sent; wait a few minutes before a re-run.' -f $state.Submits)
        }
        throw $msg
    } finally {
        Clear-SsdClipboard
        [Array]::Clear($otp.Key, 0, $otp.Key.Length)
    }
    $cert = Get-SsdCertificate $thumb
    Write-Host ('logged in: certificate {0}, {1}, private key {2}, valid until {3:yyyy-MM-dd}' -f $cert.Thumbprint, $cert.GetNameInfo([Security.Cryptography.X509Certificates.X509NameType]::SimpleName, $false), $cert.HasPrivateKey, $cert.NotAfter)
}

function Set-SsdRegistry {
    # show the login window on the first start, English UI, no autostart
    $key = $SsdConfig.RegistryKey
    if (-not (Test-Path -LiteralPath $key)) { [void](New-Item -Path $key -Force) }
    $values = [ordered]@{
        ShowLoginDialogOnStart             = 1
        ShowLoginDialogOnAppRequest        = 1
        RememberLastUserName               = 1
        Autostart                          = 0
        UnregisterCertificatesOnDisconnect = 0
        RememberPINinCSP                   = 1
        ForgetPINinCSPonDisconnect         = 1
        LangID                             = 9
    }
    foreach ($name in $values.Keys) {
        [void](New-ItemProperty -LiteralPath $key -Name $name -PropertyType DWord -Value $values[$name] -Force)
    }
    Write-Host 'registry preset written'
}

function Disconnect-SimplySign {
    # teardown for if: always(); never fails the job
    [CmdletBinding()]
    param()
    try {
        Stop-SsdProcess -GraceSeconds 5
        Write-Host 'SimplySign Desktop stopped'
    } catch {
        Write-Host "::warning::could not stop SimplySign Desktop: $($_.Exception.Message)"
    }
    Clear-SsdClipboard
    try {
        if (Test-Path -LiteralPath $SsdConfig.RegistryKey) { Remove-Item -LiteralPath $SsdConfig.RegistryKey -Recurse -Force }
    } catch {
        Write-Host "::warning::could not remove the SimplySign registry key: $($_.Exception.Message)"
    }
}

# ---------------------------------------------------------------- signing

function Find-SignTool {
    # newest x64 signtool of the Windows 10/11 SDK
    $roots = @()
    foreach ($reg in 'HKLM:\SOFTWARE\Microsoft\Windows Kits\Installed Roots', 'HKLM:\SOFTWARE\WOW6432Node\Microsoft\Windows Kits\Installed Roots') {
        try { $roots += [string](Get-ItemProperty -LiteralPath $reg -Name KitsRoot10 -ErrorAction Stop).KitsRoot10 } catch { }
    }
    $roots += (Join-Path ${env:ProgramFiles(x86)} 'Windows Kits\10')
    foreach ($root in @($roots | Where-Object { $_ } | Select-Object -Unique)) {
        $bin = Join-Path $root 'bin'
        if (-not (Test-Path -LiteralPath $bin)) { continue }
        $dirs = @(Get-ChildItem -LiteralPath $bin -Directory -ErrorAction SilentlyContinue |
                Where-Object { $_.Name -match '^10\.0\.\d+\.\d+$' } |
                Sort-Object { [version]$_.Name } -Descending)
        foreach ($d in $dirs) {
            $st = Join-Path $d.FullName 'x64\signtool.exe'
            if (Test-Path -LiteralPath $st) { return $st }
        }
    }
    throw 'No x64 signtool.exe found in the Windows SDK.'
}

function Invoke-CodeSign {
    # Sign a copy of Unsigned as Signed; a try counts only with a timestamp.
    # A failed try waits 10, 20, 40, 60, then 90 s, and no try starts later
    # than RetrySeconds after the first (sign.yml gives the step 7 minutes).
    # The login stays valid meanwhile (about 2 hours, azdocs
    # Connect-SimplySign.ps1), so waiting out a short timestamp server outage
    # beats a re-run, which needs a new approval and a new login code.
    [CmdletBinding()]
    param(
        [Parameter(Mandatory = $true)][string]$Unsigned,
        [Parameter(Mandatory = $true)][string]$Signed,
        [string]$Thumbprint = $env:CERT_SHA1,
        [int[]]$Pauses = @(10, 20, 40, 60, 90),
        [int]$RetrySeconds = 240
    )
    Set-StrictMode -Version 2
    $ErrorActionPreference = 'Stop'
    $thumb = ([string]$Thumbprint -replace '[^0-9A-Fa-f]', '').ToUpperInvariant()
    $signtool = Find-SignTool
    $tsa = $SsdConfig.TimestampUrl
    Write-Host "signtool: $signtool"
    $dir = Split-Path -Parent $Signed
    if ($dir -and -not (Test-Path -LiteralPath $dir)) { [void](New-Item -ItemType Directory -Path $dir -Force) }
    $sw = [Diagnostics.Stopwatch]::StartNew()
    $signedNoStamp = 0
    for ($i = 1; ; $i++) {
        Copy-Item -LiteralPath $Unsigned -Destination $Signed -Force
        & $signtool sign /sha1 $thumb /fd sha256 /tr $tsa /td sha256 /v $Signed
        $rc = $LASTEXITCODE
        $sig = Get-AuthenticodeSignature -LiteralPath $Signed
        $stamped = $null -ne $sig.TimeStamperCertificate
        if ($rc -eq 0 -and $sig.Status -eq 'Valid' -and $stamped) {
            Write-Host ('signed on try {0} after {1:N0} s' -f $i, $sw.Elapsed.TotalSeconds)
            return
        }
        if ($sig.Status -eq 'Valid' -and -not $stamped) { $signedNoStamp++ }
        Write-Host ('::warning::signing try {0}: signtool exit {1}, signature {2}, timestamp {3}' -f $i, $rc, $sig.Status, $stamped)
        if ($i -gt $Pauses.Count -or ($sw.Elapsed.TotalSeconds + $Pauses[$i - 1]) -gt $RetrySeconds) { break }
        Write-Host ('next try in {0} s' -f $Pauses[$i - 1])
        Start-Sleep -Seconds $Pauses[$i - 1]
    }
    $what = 'Signing failed after {0} tries in {1:N0} s' -f $i, $sw.Elapsed.TotalSeconds
    if ($signedNoStamp -gt 0) {
        throw ("$what. The certificate signed, but the timestamp server $tsa did not give a timestamp, " +
            'so the SimplySign login was fine. Re-run when the timestamp server works again ' +
            '(a re-run needs a new approval and sends a new login code).')
    }
    throw ("$what (last try: signtool exit $rc, signature $($sig.Status)). If signtool says above that the timestamp " +
        "server $tsa could not be reached, the login was fine and only the timestamp server failed.")
}

function Test-CodeSignature {
    # signtool verify plus Authenticode status, pinned signer and a timestamp
    [CmdletBinding()]
    param([Parameter(Mandatory = $true)][string]$Path, [string]$Thumbprint = $env:CERT_SHA1)
    Set-StrictMode -Version 2
    $ErrorActionPreference = 'Stop'
    $thumb = ([string]$Thumbprint -replace '[^0-9A-Fa-f]', '').ToUpperInvariant()
    $signtool = Find-SignTool
    & $signtool verify /pa /tw /v $Path
    if ($LASTEXITCODE -ne 0) { throw "signtool verify exit code $LASTEXITCODE (2 means a warning, such as no timestamp)." }
    $sig = Get-AuthenticodeSignature -LiteralPath $Path
    if ($sig.Status -ne 'Valid') { throw "Authenticode status $($sig.Status): $($sig.StatusMessage)" }
    if ($sig.SignerCertificate.Thumbprint -ne $thumb) { throw "Signed by $($sig.SignerCertificate.Thumbprint), expected $thumb." }
    if ($null -eq $sig.TimeStamperCertificate) { throw 'The signature has no timestamp.' }
    $simple = [Security.Cryptography.X509Certificates.X509NameType]::SimpleName
    Write-Host ('signature OK: signer {0}, timestamp by {1}' -f $sig.SignerCertificate.GetNameInfo($simple, $false), $sig.TimeStamperCertificate.GetNameInfo($simple, $false))
}

if ($SelfTest) {
    if (-not (Test-Totp)) { throw 'TOTP self-test failed.' }
}
