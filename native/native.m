#import <Cocoa/Cocoa.h>

NSTextField *label;
NSProgressIndicator *progress;

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
  int windowHeight = imageHeight + 200;

  NSWindow* window = [[[NSWindow alloc] initWithContentRect:NSMakeRect(0, 0, imageWidth, windowHeight)
    styleMask:(NSWindowStyleMaskTitled | NSWindowStyleMaskClosable) backing:NSBackingStoreBuffered defer:NO]
    autorelease];

  // main image
  NSImageView *imageView = [[NSImageView new] autorelease];
  NSImage *image = [NSImage imageNamed:@"installer.png"];
  [imageView setImage:image];
  [imageView setFrame:CGRectMake(0,windowHeight-imageHeight,imageWidth,imageHeight)];
  [window.contentView addSubview:imageView];

  int bottomMargin = 20;

  // progress bar
  progress = [[NSProgressIndicator new] autorelease];
  int progressMargin = 30;
  [progress setFrame:CGRectMake(progressMargin,bottomMargin+40,imageWidth-progressMargin*2,60)];
  [progress setIndeterminate:NO];
  [progress setMinValue:0.0];
  [progress setMaxValue:100.0];
  [window.contentView addSubview:progress];

  // progress label
  label = [[NSTextField new] autorelease];
  [label setFrame:CGRectMake(0,bottomMargin,imageWidth,40)];
  [label setAlignment:NSTextAlignmentCenter];
  [label setBezeled:NO];
  [label setDrawsBackground:NO];
  [label setEditable:NO];
  [label setSelectable:NO];
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

void SetPercent(int percent) {
  dispatch_async(dispatch_get_main_queue(), ^(void) {
    double dpercent = (double) percent;
    [progress setDoubleValue:dpercent];
    [progress display];
  });
}

