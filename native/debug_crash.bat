@echo off
"C:\Program Files (x86)\Windows Kits\10\Debuggers\x86\cdb.exe" -G -g -c ".symfix; .reload; .dump /ma D:\pivox\crash.dmp; !analyze -v; kL; q" D:\tools\TstCon.exe D:\tools\pivox.tcs
