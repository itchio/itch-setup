
all:
	windres -o itchSetup.syso itchSetup.rc
	go get -v -x -ldflags="-H windowsgui"
	#upx -1 ${GOPATH}/bin/itchSetup.exe
