#import <Cocoa/Cocoa.h>

int Relaunch(char *cAppPath, char *cPID) {
	NSAutoreleasePool *pool = [[NSAutoreleasePool alloc] init];

	pid_t parentPID = atoi(cPID);
	ProcessSerialNumber psn;
	while (GetProcessForPID(parentPID, &psn) != procNotFound)
		sleep(1);

	NSString *appPath = [NSString stringWithCString:cAppPath encoding:NSUTF8StringEncoding];
	BOOL success = [[NSWorkspace sharedWorkspace] openFile:[appPath stringByExpandingTildeInPath]];

	if (!success)
		NSLog(@"Error: could not relaunch application at %@", appPath);

	[pool drain];
	return (success) ? 0 : 1;
}
