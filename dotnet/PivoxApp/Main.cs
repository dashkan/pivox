using AppKit;
using PivoxApp;

// All-code AppKit bootstrap. Without a storyboard or MainMenu nib,
// NSApplicationMain has no mechanism to discover and instantiate
// the AppDelegate class. We construct it explicitly here, assign
// it to the shared app, then hand control to NSApplication.Main.
NSApplication.Init();
NSApplication.SharedApplication.Delegate = new AppDelegate();
NSApplication.Main(args);
