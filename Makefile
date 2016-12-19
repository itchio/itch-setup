
all:
	windres -o itchSetup.syso itchSetup.rc
	go get -v -x -ldflags="-H windowsgui"
	#upx --best ${GOPATH}/bin/itchSetup.exe
