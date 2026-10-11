cd /d %~dp0
set GOARCH=386
set CGO_ENABLED=0
go build -tags timetzdata,server -o .\bin\joyWaApi.exe .\app
