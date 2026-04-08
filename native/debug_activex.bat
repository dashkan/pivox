@echo off
REM Run test container, auto-dismiss MessageBox after 2 seconds, capture crash dump
"C:\Program Files (x86)\Windows Kits\10\Debuggers\x86\cdb.exe" -G -g -c ".symfix;.reload;sxe -c \".dump /ma D:\\pivox\\crash.dmp;!analyze -v;kL;q\" av;sxe -c \".dump /ma D:\\pivox\\crash.dmp;!analyze -v;kL;q\" eh;g" D:\tools\TstCon.exe D:\tools\pivox.tcs
