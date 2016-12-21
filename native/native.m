#import <Cocoa/Cocoa.h>

NSTextField *label;
NSProgressIndicator *progressIndicator;

extern void StartItchSetup(void);

int StartApp(void) {
  [NSAutoreleasePool new];
  [NSApplication sharedApplication];
  [NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];

  // build menu
  id menubar = [[NSMenu new] autorelease];
  id appMenuItem = [[NSMenuItem new] autorelease];
  [menubar addItem:appMenuItem];
  [NSApp setMainMenu:menubar];
  id appMenu = [[NSMenu new] autorelease];
  id appName = @"itch Setup";
  id quitTitle = [@"Quit " stringByAppendingString:appName];
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
  NSImage *image = [NSImage imageNamed:@"installer.png"];
  [image setSize:NSMakeSize(imageWidth, imageHeight)];
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
  [window setTitle:appName];
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

void Relaunch(char *cBundlePath) {
  NSBundle *bundle = [NSBundle mainBundle];
  NSString* relaunchPath = [bundle pathForResource:@"relaunch" ofType:nil];

  id bundlePath = [NSString stringWithUTF8String:cBundlePath];

  [NSTask launchedTaskWithLaunchPath:relaunchPath arguments:[NSArray arrayWithObjects:bundlePath, [NSString stringWithFormat:@"%d", [[NSProcessInfo processInfo] processIdentifier]], nil]];
}

void Quit() {
  [NSApp terminate:nil];
}

