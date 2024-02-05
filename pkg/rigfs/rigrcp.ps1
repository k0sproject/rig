begin {
  Set-Alias NO New-Object

  function Close-Dispose($obj){
    if ($obj -ne $null){
      $obj.Close()
      $obj.Dispose()
    }
  }

  function Emit($s, $obj){
    $j=ConvertTo-Json -InputObject $obj -Depth 10 -Compress
    $b=[System.Text.Encoding]::UTF8.GetBytes($j + [char]0)
    $s.Write($b, 0, $b.Length)
    $s.Flush()
  }

  function Invoke-WithRetry($Script, $Retries, $Match) {
      for ($i = 1; $i -le $Retries; $i++) {
          try {
              &$Script
              return
          } catch {
              if ($_.Exception.Message -like $Match) {
                  Write-Warning "retrying"
                  Start-Sleep -Seconds 1
              } else {
                  throw
              }
          }
      }
  }

  function HexDump {
      # Assuming the first argument to the function is the buffer
      $buffer = $args[0]

      for ($i = 0; $i -lt $buffer.Length; $i += 16) {
          $line = $buffer[$i..([Math]::Min($i + 15, $buffer.Length - 1))]
          $hex = ($line | ForEach-Object { "{0:X2}" -f $_ }) -join " "
          $text = ($line | ForEach-Object { if ($_ -ge 32 -and $_ -le 126) {[char]$_} else {"."} }) -join ""
          $offset = "{0:X8}" -f $i
          "${offset}: $hex  $text"
      }
  }
  $DebugPreference="Continue"
  $ErrorActionPreference="Stop"
  $ProgressPreference="SilentlyContinue"

  # import the GetStdHandle function from kernel32.dll to get a raw stdin handle
  # it seems this kills the powershell window when the script is done, but that
  # is acceptable as this is only run in a dedicated shell
  $MethodDefinitions=@'
[DllImport("kernel32.dll", SetLastError=true)]
public static extern IntPtr GetStdHandle(int nStdHandle);
'@
  $Kernel32=Add-Type -MemberDefinition $MethodDefinitions -Name 'Kernel32' -Namespace 'Win32' -PassThru
  $inHandle=$Kernel32::GetStdHandle(-10)
  $outHandle = $Kernel32::GetStdHandle(-11)

  $in=NO IO.FileStream $inHandle, ([IO.FileAccess]::Read), ([IO.FileShare]::Read), 16384, $false
  $inStream=NO IO.StreamReader $in

  $out = New-Object IO.FileStream $outHandle, ([IO.FileAccess]::Write), ([IO.FileShare]::Write), 16384, $false

  $f=$null
  $quit=$false
  $p=$null

  while(!$inStream.EndOfStream -And !$quit){
    try {
      $command=$inStream.ReadLine()
      $arg=$command -split " "

      switch ($arg[0]){
        # open
        'o' {
          if ($f -ne $null){ throw "file already open" }
          $script:mode=$arg[1]
          $script:access=$arg[2]
          $path=$arg[3..($arg.Length-1)] -join " "
          if ([string]::IsNullOrEmpty($path)) { throw "Path argument is missing or empty" }
          # Check if the path is rooted, if not, make it absolute
          if (-not [IO.Path]::IsPathRooted($path)) {
            $path = Join-Path $pwd $path
          }
          try {
            $script:p=Resolve-Path $path -ErrorAction Stop
          } catch {
            $script:p=$path
          }
          if(Test-Path $script:p -PathType Container){
            throw "cannot open directory"
          }
          try {
            $script:fi=NO IO.FileInfo($script:p)
          } catch {
            throw "can't create FileInfo('" + $script:p + "'): " + $_.Exception.Message
          }
          $script:f=$null
          Invoke-WithRetry -Retries 10 -Match "*used by another process*" -Script { 
              $script:f=$script:fi.Open($script:mode, $script:access)
          }
          $f=$script:f
          $script:f=$null
          if ($f -eq $null){
            throw "file not opened"
          }
          $pos=$f.Position
          $o=@{
             pos=$pos
          }
          Emit $out $o
        }
        # seek
        's' {
          if ($f -eq $null){ throw "file not open" }
          $pos=$arg[1]
          $whence=$arg[2]
          $pos=$f.Seek($pos, $whence)
          $o=@{
            pos=$pos
          }
          Emit $out $o
        }
        # read
        'r' {
          if ($f -eq $null){ throw "file not open" }
          $cnt=[int]$arg[1]
          if($cnt -eq -1){
            $total=$f.Length - $f.Position
            $pos=$f.Length
            $o=@{
             n=$total
            }
            Emit $out $o
            $out.Flush()
            $f.CopyTo($out)
            $out.Flush()
            continue
          }

          if($f.EndOfStream){
            throw "eof"
          }
          
          $buf=NO byte[] $cnt
          $b=$f.Read($buf, 0, $cnt)
          if($b -eq 0){
            throw "eof"
          }
          $o=@{
             n=$b
          }
          Emit $out $o
          $out.Flush()
          $out.Write($buf, 0, $b)
          $out.Flush()
          $buf=$null
        }
        # write
        'w' {
          if ($f -eq $null){ throw "file not open" }
          $cnt=[int]$arg[1]
          $o=@{
             n=$cnt
          }
          Emit $out $o
          $out.Flush()
          $buf=NO byte[] $cnt
          $b=$in.Read($buf, 0, $cnt)
          if($b -ne $cnt){
            $dump=HexDump $buf
            throw "short read $b bytes instead of $cnt bytes\n$dump"
          }
          $f.Write($buf, 0, $b)
          $f.Flush()
          $buf=$null
        }
        'c' {
          if ($f -eq $null){ throw "file not open" }
          Close-Dispose $f
          $f=$null
          $o=@{
             pos=-1
          }
          Emit $out $o
        }
        'q' {
          $quit=$true
          Close-Dipose $out
          Close-Dipose $inStream
          Close-Dipose $in
        }
        default {
          throw "invalid command"
        }
      }
    } catch {
      $msg=@{
         error=$_.Exception.Message + " trace: " + $_.Exception.StackTrace
      }
      Emit $out $msg
    }
  }
}
