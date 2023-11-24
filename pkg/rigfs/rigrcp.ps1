begin {
  Set-Alias NO New-Object
  
  class Stat {
    [int]$size
    [int]$mode
    [int]$unixMode
    [int]$modTime
    [bool]$isDir
    [string]$name
    Stat([IO.FileSystemInfo]$fi){
      if($fi.Exists -eq $false){
        throw ("file not found")
      }
      $this.isDir=($fi.Attributes -band [IO.FileAttributes]::Directory)
      $this.modTime=[int](Get-Date ($fi.LastWriteTimeUtc).ToUniversalTime() -UFormat %s)
      $this.size=[int]$fi.Length
      $this.unixMode=[int]$fi.UnixFileMode
      $this.mode=[int]$fi.Attributes
      $this.name=$fi.FullName
    }
  }

  class FileContext {
    [IO.FileStream]$f
    [bool]$EOF

    FileContext([IO.FileStream]$f){
        $this.f=$f
    }
  }

  class FM {
    hidden [hashtable]$f = @{}

    [string] Add($ctx) {
      $id=[guid]::NewGuid().ToString()
      $this.f[$id] = $ctx
      return $id
    }

    [FileContext] Get([string]$id) {
      if ($this.f.ContainsKey($id)) {
          return $this.f[$id]
      } else {
          throw "file not open"
      }
    }

    [void] Del([string]$id) {
      if ($this.f.ContainsKey($id)) {
          $v=$this.f[$id].f
          $this.f.Remove($id)
          $v.Close()
          $v.Dispose()
          $v=$null
      }
    }

    [void] Close() {
      $this.f.Values|ForEach-Object {
        $this.Del($_)
      }
    }
  }

  function FSInfo($path){
    try {
      $p=Resolve-Path $path
    } catch {
      if(![IO.Path]::IsPathRooted($path)){
        $p=Join-Path $pwd $path
      }
    }

    if(Test-Path $p -PathType Container){
        return (NO IO.DirectoryInfo($p))
    }
    return (NO IO.FileInfo($p))
  }

  function Emit($s, $obj){
    $j=ConvertTo-Json -InputObject $obj -Depth 10 -Compress
    $b=[System.Text.Encoding]::UTF8.GetBytes($j)
    $s.Write($b, 0, $b.Length)
    $s.WriteByte(0)
  }

	$bufSize=32768
  
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
  $in=NO IO.FileStream $inHandle, ([IO.FileAccess]::Read), ([IO.FileShare]::Read), 16384, $false
  $inStream=NO IO.StreamReader $in

  [Console]::OutputEncoding=[System.Text.Encoding]::ASCII
  $out=[System.Console]::OpenStandardOutput()

  $buf=NO byte[] $bufSize
  # create a file manager
  $fm=[FM]::new()

  while(!$inStream.EndOfStream){
    try {
      $command=$inStream.ReadLine()
      $arg=$command -split " "

      switch ($arg[0]){
        'stat' {
          $p=$arg[1..($arg.Length-1)] -join " "
          $fi=FSInfo $p
          $info=NO Stat $fi
          $o=@{
            stat=$info
          }
          Emit $out $o
        }
        'dir' {
          $p=$arg[1..($arg.Length-1)] -join " "
          $di=FSInfo $p
          if($di -is [IO.FileInfo]){
            throw "not a directory"
          }
          if(!$di.Exists){
            throw "directory not found"
          }
          try {
            $di.GetAccessControl()|Out-Null
          } catch {
            throw "access denied"
          }
          $infos=@()
          $di.GetFileSystemInfos()|ForEach-Object {
            $info=NO Stat $_
            $infos += $info
          }
          $o=@{
            dir=$infos
          }
          Emit $out $o
        }
        # open
        'o' { 
          $mode=$arg[1]
          $p=$arg[2..($arg.Length-1)] -join " "
          $fi=FSInfo $p
          $p=$fi.FullName

          $fmode=$null
          $m=[IO.FileMode]
          switch ($mode){
            'ro' {$fmode=$m::Open}
            'w' {$fmode=$m::CreateNew}
            'rw' {$fmode=$m::ReadWrite}
            'c' {if($fi.Exists){$fmode=$m::Truncate} else {$fmode=$m::Create}}
            'a' {$fmode=$m::Append}
            default {throw "invalid mode"}
          }

          $f=NO IO.FileStream($p, $fmode)
          $ctx=[FileContext]::new($f)
          $id=$fm.Add($ctx)
          $props=@{
            id=$id
            pos=$ctx.f.Position
            eof=$ctx.EOF
            name=$ctx.f.Name
          }
          $o=@{
             open=$props
          }
          Emit $out $o
        }
        # seek
        'seek' {
          $ctx=$fm.Get($arg[1])
          $f=$ctx.f
          $whence=[int]$arg[2]
          $pos=$arg[3]
          $cp=$f.Position
          switch ($whence){
            0 {$cp=$pos}
            1 {$cp+=$pos}
            2 {$cp=$f.Length-[Math]::Abs($pos)}
            default {
              throw "invalid whence"
            }
          }
          $f.Position=$cp
          $ctx.EOF=$cp -ge $f.Length
          $props=@{
            position=[int64]$cp
          }
          $o=@{
             seek=$props
          }
          Emit $out $o
        }
        # read
        'r' {
          $ctx=$fm.Get($arg[1])
          if($ctx.EOF){
            throw "eof"
          }
          if(-not ($f -is [IO.FileStream] -and $f.CanRead)){
            throw "file not open for writing"
          }

          $cnt=[int]$arg[2]
          if($cnt -eq 0){
            throw "zero count"
          }

          $f=$ctx.f

          if($cnt -eq -1){
            $total=$f.Length - $f.Position
            $pos=$f.Length
            $props=@{
              bytes=$total
            }
            $o=@{
               read=$props
              }
            Emit $out $o
            $out.Flush()
            $f.CopyTo($out)
            $out.Flush()
            $ctx.EOF=$true
            continue
          }

          if($cnt -gt $bufSize){ 
            throw ("count exceeds buffer size "+$bufSize)
          }

          if($f.EndOfStream){
            $ctx.EOF=$true
            throw "eof"
          }

          $b=$f.Read($buf, 0, $count)

          if($b -eq 0){
            $ctx.EOF=$true
            throw "eof"
          }
          $props=@{
            bytes=$b
          }
          $o=@{
             read=$props
          }
          Emit $out $o
          $out.Flush()
          $out.Write($buf, 0, $b)
          $out.Flush()
        }
        # write
        'w' {
          $ctx=$fm.Get($arg[1])
          $f=$ctx.f
          if(-not ($f -is [IO.FileStream] -and $f.CanWrite)){
            throw "file not open for writing"
          }

          $count=[int]$arg[2]
          if($count -eq 0){
            throw "zero count"
          }
          $out.WriteByte(0)

          $rt=0
          while ($rt -lt $count){
            $toRead=[Math]::Min($bufSize, $count - $rt)
            $r=$in.Read($buf, 0, $toRead)
            $f.Write($buf, 0, $r)
            $rt += $r
          }
          $ctx.EOF=$true
        }
        # close
        'c' {
          $fm.Del($arg[1])
          $out.WriteByte(0)
        }
        'q' {
          throw "quit"
        }
        default {
          throw "invalid command"
        }
      }
    } catch {
      if($_.Exception.Message -eq "quit"){
        break
      }
      $msg=@{
         error=$_.Exception.Message
      }
      Emit $out $msg
    }
  }
  $fm.Close()
}
