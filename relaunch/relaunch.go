package main

/*
int Relaunch(char *appPath, char *pid);
*/
import "C"

import "os"

func main() {
	result := C.Relaunch(C.CString(os.Args[1]), C.CString(os.Args[2]))
	os.Exit(int(result))
}
