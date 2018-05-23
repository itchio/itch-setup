#import <Cocoa/Cocoa.h>

NSTextField *label;
NSProgressIndicator *progressIndicator;

extern void StartItchSetup(void);

int StartApp(char *cSetupTitle, char *cAppName, char *imageBytes, int imageLen) {
  [NSAutoreleasePool new];
  [NSApplication sharedApplication];
  [NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];

  // build menu
  id menubar = [[NSMenu new] autorelease];
  id appMenuItem = [[NSMenuItem new] autorelease];
  [menubar addItem:appMenuItem];
  [NSApp setMainMenu:menubar];
  id appMenu = [[NSMenu new] autorelease];
  id setupTitle = [NSString stringWithUTF8String:cSetupTitle];
  id quitTitle = [@"Quit " stringByAppendingString:setupTitle];
  id quitMenuItem = [[[NSMenuItem alloc] initWithTitle:quitTitle
    action:@selector(terminate:) keyEquivalent:@"q"]
    autorelease];
  [appMenu addItem:quitMenuItem];
  [appMenuItem setSubmenu:appMenu];

  int imageWidth = 622;
  int imageHeight = 301;
  int windowHeight = imageHeight + 85;

  NSWindow* window = [[[NSWindow alloc] initWithContentRect:NSMakeRect(0, 0, imageWidth, windowHeight)
    styleMask:(NSTitledWindowMask | NSClosableWindowMask) backing:NSBackingStoreBuffered defer:NO]
    autorelease];

  // main image
  NSImageView *imageView = [[NSImageView new] autorelease];

  id appName = [NSString stringWithUTF8String:cAppName];

  id imageData = [NSData dataWithBytes:imageBytes length:imageLen];
  NSBitmapImageRep *imageRep = [NSBitmapImageRep imageRepWithData:imageData];
  NSSize imageSize = NSMakeSize(CGImageGetWidth([imageRep CGImage]), CGImageGetHeight([imageRep CGImage]));

  NSImage *image = [[NSImage alloc] initWithSize:imageSize];
  [image addRepresentation:imageRep]; 
  
  [imageView setImage:image];
  [imageView setFrame:CGRectMake(0,windowHeight-imageHeight,imageWidth,imageHeight)];
  [window.contentView addSubview:imageView];

  int bottomMargin = 10;
  int labelHeight = 28;
  int indicatorHeight = 40;

  // progress bar
  progressIndicator = [[NSProgressIndicator new] autorelease];
  int progressMargin = 30;
  [progressIndicator setFrame:CGRectMake(progressMargin,bottomMargin+labelHeight,imageWidth-progressMargin*2,indicatorHeight)];
  [progressIndicator setIndeterminate:YES];
  [progressIndicator setMinValue:0.0];
  [progressIndicator setMaxValue:1000.0];
  [window.contentView addSubview:progressIndicator];

  // progress label
  label = [[NSTextField new] autorelease];
  [label setFrame:CGRectMake(0,bottomMargin,imageWidth,labelHeight)];
  [label setAlignment:NSCenterTextAlignment];
  [label setBezeled:NO];
  [label setDrawsBackground:NO];
  [label setEditable:NO];
  [label setSelectable:NO];
  [label setStringValue:@"Warming up..."];
  [window.contentView addSubview:label];

  // finish window setup
  [window center];
  [window setTitle:setupTitle];
  [window makeKeyAndOrderFront:nil];
  [NSApp activateIgnoringOtherApps:YES];

  StartItchSetup();

  [NSApp run];
  return 0;
}

void SetLabel(char *cString) {
  dispatch_async(dispatch_get_main_queue(), ^(void) {
    @autoreleasepool {
      id string = [NSString stringWithUTF8String:cString];
      [label setStringValue:string];
    }
  });
}

void SetProgress(int progress) {
  dispatch_async(dispatch_get_main_queue(), ^(void) {
    [progressIndicator setIndeterminate:NO];
    double doubleValue = (double) progress;
    [progressIndicator setDoubleValue:doubleValue];
  });
}

char *ValidateBundle(char *cBundlePath) {
  id bundlePath = [NSString stringWithUTF8String:cBundlePath];
  NSURL* bundleURL = [NSURL fileURLWithPath:bundlePath];

  SecStaticCodeRef staticCode = NULL;

  OSStatus result = SecStaticCodeCreateWithPath((__bridge CFURLRef)bundleURL, kSecCSDefaultFlags, &staticCode);

  if (result != noErr) {
    NSLog(@"Failed to get static code for bundle %@", bundleURL);
    return "Failed to get static code for bundle";
  }

  CFErrorRef validityError = NULL;
  result = SecStaticCodeCheckValidityWithErrors(staticCode, kSecCSCheckAllArchitectures, nil, &validityError);

  if (result != noErr) {
    NSLog(@"Bundle %@ isn't signed/valid: %@", bundleURL, CFErrorCopyDescription(validityError));
    return "Bundle isn't signed/valid";
  }

  return nil;
}

int LaunchBundle(char *cBundlePath) {
  id bundlePath = [NSString stringWithUTF8String:cBundlePath];
  NSLog(@"Opening bundle %@", bundlePath);
  BOOL success = [[NSWorkspace sharedWorkspace] launchApplication:bundlePath];
  NSLog(@"Success? %@", success ? @"yes" : @"no");

  return success ? 1 : 0;
}

void Quit() {
  [NSApp terminate:nil];
}

