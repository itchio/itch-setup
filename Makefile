UPX_LEVEL?=-1

all:
	windres -o itchSetup.syso itchSetup.rc
	go get -v -x -ldflags="-H windowsgui"
	upx ${UPX_LEVEL} ${GOPATH}/bin/itchSetup.exe
