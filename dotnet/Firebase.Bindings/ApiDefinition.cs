using System;
using FirebaseCore;
using Foundation;
using ObjCRuntime;

namespace FirebaseCore {
	// typedef void (^FIRAppVoidBoolCallback)(BOOL);
	delegate void FIRAppVoidBoolCallback (bool arg0);

	// @interface FIRApp : NSObject
	[BaseType (typeof (NSObject))]
	[DisableDefaultCtor]
	interface FIRApp {
		// +(void)configure;
		[Static]
		[Export ("configure")]
		void Configure ();

		// +(void)configureWithOptions:(FIROptions * _Nonnull)options __attribute__((swift_name("configure(options:)")));
		[Static]
		[Export ("configureWithOptions:")]
		void ConfigureWithOptions (FIROptions options);

		// +(void)configureWithName:(NSString * _Nonnull)name options:(FIROptions * _Nonnull)options __attribute__((swift_name("configure(name:options:)")));
		[Static]
		[Export ("configureWithName:options:")]
		void ConfigureWithName (string name, FIROptions options);

		// +(FIRApp * _Nullable)defaultApp __attribute__((swift_name("app()")));
		[Static]
		[NullAllowed, Export ("defaultApp")]
		FIRApp DefaultApp { get; }

		// +(FIRApp * _Nullable)appNamed:(NSString * _Nonnull)name __attribute__((swift_name("app(name:)")));
		[Static]
		[Export ("appNamed:")]
		[return: NullAllowed]
		FIRApp AppNamed (string name);

		// @property (readonly, class) NSDictionary<NSString *,FIRApp *> * _Nullable allApps;
		[Static]
		[NullAllowed, Export ("allApps")]
		NSDictionary AllApps { get; }

		// -(void)deleteApp:(void (^ _Nonnull)(BOOL))completion;
		[Export ("deleteApp:")]
		void DeleteApp (Action<bool> completion);

		// @property (readonly, copy, nonatomic) NSString * _Nonnull name;
		[Export ("name")]
		string Name { get; }

		// @property (readonly, copy, nonatomic) FIROptions * _Nonnull options;
		[Export ("options", ArgumentSemantic.Copy)]
		FIROptions Options { get; }

		// @property (getter = isDataCollectionDefaultEnabled, readwrite, nonatomic) BOOL dataCollectionDefaultEnabled;
		[Export ("dataCollectionDefaultEnabled")]
		bool DataCollectionDefaultEnabled { [Bind ("isDataCollectionDefaultEnabled")] get; set; }
	}

	// @interface FIRConfiguration : NSObject
	[BaseType (typeof (NSObject))]
	interface FIRConfiguration {
		// @property (readonly, nonatomic, class) NS_SWIFT_NAME(shared) FIRConfiguration * sharedInstance __attribute__((swift_name("shared")));
		[Static]
		[Export ("sharedInstance")]
		FIRConfiguration SharedInstance { get; }

		// -(FIRLoggerLevel)loggerLevel;
		// -(void)setLoggerLevel:(FIRLoggerLevel)loggerLevel;
		[Export ("loggerLevel")]
		FIRLoggerLevel LoggerLevel { get; set; }
	}

	// @interface FIROptions : NSObject <NSCopying>
	[BaseType (typeof (NSObject))]
	[DisableDefaultCtor]
	interface FIROptions : INSCopying {
		// +(FIROptions * _Nullable)defaultOptions __attribute__((swift_name("defaultOptions()")));
		[Static]
		[NullAllowed, Export ("defaultOptions")]
		FIROptions DefaultOptions { get; }

		// @property (copy, nonatomic) NS_SWIFT_NAME(apiKey) NSString * APIKey __attribute__((swift_name("apiKey")));
		[Export ("APIKey")]
		string APIKey { get; set; }

		// @property (copy, nonatomic) NSString * _Nonnull bundleID;
		[Export ("bundleID")]
		string BundleID { get; set; }

		// @property (copy, nonatomic) NSString * _Nullable clientID;
		[NullAllowed, Export ("clientID")]
		string ClientID { get; set; }

		// @property (copy, nonatomic) NS_SWIFT_NAME(gcmSenderID) NSString * GCMSenderID __attribute__((swift_name("gcmSenderID")));
		[Export ("GCMSenderID")]
		string GCMSenderID { get; set; }

		// @property (copy, nonatomic) NSString * _Nullable projectID;
		[NullAllowed, Export ("projectID")]
		string ProjectID { get; set; }

		// @property (copy, nonatomic) NSString * _Nonnull googleAppID;
		[Export ("googleAppID")]
		string GoogleAppID { get; set; }

		// @property (copy, nonatomic) NSString * _Nullable databaseURL;
		[NullAllowed, Export ("databaseURL")]
		string DatabaseURL { get; set; }

		// @property (copy, nonatomic) NSString * _Nullable storageBucket;
		[NullAllowed, Export ("storageBucket")]
		string StorageBucket { get; set; }

		// @property (copy, nonatomic) NSString * _Nullable appGroupID;
		[NullAllowed, Export ("appGroupID")]
		string AppGroupID { get; set; }

		// -(instancetype _Nullable)initWithContentsOfFile:(NSString * _Nonnull)plistPath __attribute__((objc_designated_initializer));
		[Export ("initWithContentsOfFile:")]
		[DesignatedInitializer]
		NativeHandle Constructor (string plistPath);

		// -(instancetype _Nonnull)initWithGoogleAppID:(NSString * _Nonnull)googleAppID GCMSenderID:(NSString * _Nonnull)GCMSenderID __attribute__((swift_name("init(googleAppID:gcmSenderID:)"))) __attribute__((objc_designated_initializer));
		[Export ("initWithGoogleAppID:GCMSenderID:")]
		[DesignatedInitializer]
		NativeHandle Constructor (string googleAppID, string GCMSenderID);
	}

	// @interface FIRTimestamp : NSObject <NSCopying>
	[BaseType (typeof (NSObject))]
	[DisableDefaultCtor]
	interface FIRTimestamp : INSCopying {
		// -(instancetype _Nonnull)initWithSeconds:(int64_t)seconds nanoseconds:(int32_t)nanoseconds __attribute__((objc_designated_initializer));
		[Export ("initWithSeconds:nanoseconds:")]
		[DesignatedInitializer]
		NativeHandle Constructor (long seconds, int nanoseconds);

		// +(instancetype _Nonnull)timestampWithSeconds:(int64_t)seconds nanoseconds:(int32_t)nanoseconds;
		[Static]
		[Export ("timestampWithSeconds:nanoseconds:")]
		FIRTimestamp TimestampWithSeconds (long seconds, int nanoseconds);

		// +(instancetype _Nonnull)timestampWithDate:(NSDate * _Nonnull)date;
		[Static]
		[Export ("timestampWithDate:")]
		FIRTimestamp TimestampWithDate (NSDate date);

		// +(instancetype _Nonnull)timestamp;
		[Static]
		[Export ("timestamp")]
		FIRTimestamp Timestamp ();

		// -(NSDate * _Nonnull)dateValue;
		[Export ("dateValue")]
		NSDate DateValue { get; }

		// -(NSComparisonResult)compare:(FIRTimestamp * _Nonnull)other;
		[Export ("compare:")]
		NSComparisonResult Compare (FIRTimestamp other);

		// @property (readonly, assign, nonatomic) int64_t seconds;
		[Export ("seconds")]
		long Seconds { get; }

		// @property (readonly, assign, nonatomic) int32_t nanoseconds;
		[Export ("nanoseconds")]
		int Nanoseconds { get; }
	}

	[Static]
	partial interface Constants {
		// extern double FirebaseCoreVersionNumber;
		[Field ("FirebaseCoreVersionNumber", "__Internal")]
		double FirebaseCoreVersionNumber { get; }// 
// 
// 		// extern const unsigned char[] FirebaseCoreVersionString;
// 		[Field ("FirebaseCoreVersionString", "__Internal")]
// 		byte [] FirebaseCoreVersionString { get; }
// 
	}
}


namespace FirebaseAuth {
	// typedef void (^FIRUserUpdateCallback)(NSError * _Nullable);
	delegate void FIRUserUpdateCallback ([NullAllowed] NSError arg0);

	// typedef void (^FIRAuthStateDidChangeListenerBlock)(FIRAuth * _Nonnull, FIRUser * _Nullable);
	delegate void FIRAuthStateDidChangeListenerBlock (FIRAuth arg0, [NullAllowed] FIRUser arg1);

	// typedef void (^FIRIDTokenDidChangeListenerBlock)(FIRAuth * _Nonnull, FIRUser * _Nullable);
	delegate void FIRIDTokenDidChangeListenerBlock (FIRAuth arg0, [NullAllowed] FIRUser arg1);

	// typedef void (^FIRAuthDataResultCallback)(FIRAuthDataResult * _Nullable, NSError * _Nullable);
	delegate void FIRAuthDataResultCallback ([NullAllowed] FIRAuthDataResult arg0, [NullAllowed] NSError arg1);

	partial interface Constants {
		// extern NS_SWIFT_NAME(AuthStateDidChange) const NSNotificationName FIRAuthStateDidChangeNotification __attribute__((swift_name("AuthStateDidChange")));
		[Field ("FIRAuthStateDidChangeNotification", "__Internal")]
		NSString FIRAuthStateDidChangeNotification { get; }
	}

	// typedef void (^FIRAuthResultCallback)(FIRUser * _Nullable, NSError * _Nullable);
	delegate void FIRAuthResultCallback ([NullAllowed] FIRUser arg0, [NullAllowed] NSError arg1);

	// typedef void (^FIRProviderQueryCallback)(NSArray<NSString *> * _Nullable, NSError * _Nullable);
	delegate void FIRProviderQueryCallback ([NullAllowed] string [] arg0, [NullAllowed] NSError arg1);

	// typedef void (^FIRSignInMethodQueryCallback)(NSArray<NSString *> * _Nullable, NSError * _Nullable);
	delegate void FIRSignInMethodQueryCallback ([NullAllowed] string [] arg0, [NullAllowed] NSError arg1);

	// typedef void (^FIRSendPasswordResetCallback)(NSError * _Nullable);
	delegate void FIRSendPasswordResetCallback ([NullAllowed] NSError arg0);

	// typedef void (^FIRSendSignInLinkToEmailCallback)(NSError * _Nullable);
	delegate void FIRSendSignInLinkToEmailCallback ([NullAllowed] NSError arg0);

	// typedef void (^FIRConfirmPasswordResetCallback)(NSError * _Nullable);
	delegate void FIRConfirmPasswordResetCallback ([NullAllowed] NSError arg0);

	// typedef void (^FIRVerifyPasswordResetCodeCallback)(NSString * _Nullable, NSError * _Nullable);
	delegate void FIRVerifyPasswordResetCodeCallback ([NullAllowed] string arg0, [NullAllowed] NSError arg1);

	// typedef void (^FIRApplyActionCodeCallback)(NSError * _Nullable);
	delegate void FIRApplyActionCodeCallback ([NullAllowed] NSError arg0);

	// typedef void (^FIRAuthVoidErrorCallback)(NSError * _Nullable);
	delegate void FIRAuthVoidErrorCallback ([NullAllowed] NSError arg0);

	partial interface Constants {
		// extern NS_SWIFT_NAME(AuthErrorDomain) NSString *const FIRAuthErrorDomain __attribute__((swift_name("AuthErrorDomain")));
		[Field ("FIRAuthErrorDomain", "__Internal")]
		NSString FIRAuthErrorDomain { get; }

		// extern NS_SWIFT_NAME(AuthErrorUserInfoNameKey) NSString *const FIRAuthErrorUserInfoNameKey __attribute__((swift_name("AuthErrorUserInfoNameKey")));
		[Field ("FIRAuthErrorUserInfoNameKey", "__Internal")]
		NSString FIRAuthErrorUserInfoNameKey { get; }

		// extern NS_SWIFT_NAME(AuthErrorUserInfoEmailKey) NSString *const FIRAuthErrorUserInfoEmailKey __attribute__((swift_name("AuthErrorUserInfoEmailKey")));
		[Field ("FIRAuthErrorUserInfoEmailKey", "__Internal")]
		NSString FIRAuthErrorUserInfoEmailKey { get; }

		// extern NS_SWIFT_NAME(AuthErrorUserInfoUpdatedCredentialKey) NSString *const FIRAuthErrorUserInfoUpdatedCredentialKey __attribute__((swift_name("AuthErrorUserInfoUpdatedCredentialKey")));
		[Field ("FIRAuthErrorUserInfoUpdatedCredentialKey", "__Internal")]
		NSString FIRAuthErrorUserInfoUpdatedCredentialKey { get; }

		// extern NS_SWIFT_NAME(AuthErrorUserInfoMultiFactorResolverKey) NSString *const FIRAuthErrorUserInfoMultiFactorResolverKey __attribute__((swift_name("AuthErrorUserInfoMultiFactorResolverKey")));
		[Field ("FIRAuthErrorUserInfoMultiFactorResolverKey", "__Internal")]
		NSString FIRAuthErrorUserInfoMultiFactorResolverKey { get; }

		// extern NS_SWIFT_NAME(EmailAuthProviderID) NSString *const FIREmailAuthProviderID __attribute__((swift_name("EmailAuthProviderID")));
		[Field ("FIREmailAuthProviderID", "__Internal")]
		NSString FIREmailAuthProviderID { get; }

		// extern NS_SWIFT_NAME(EmailLinkAuthSignInMethod) NSString *const FIREmailLinkAuthSignInMethod __attribute__((swift_name("EmailLinkAuthSignInMethod")));
		[Field ("FIREmailLinkAuthSignInMethod", "__Internal")]
		NSString FIREmailLinkAuthSignInMethod { get; }

		// extern NS_SWIFT_NAME(EmailPasswordAuthSignInMethod) NSString *const FIREmailPasswordAuthSignInMethod __attribute__((swift_name("EmailPasswordAuthSignInMethod")));
		[Field ("FIREmailPasswordAuthSignInMethod", "__Internal")]
		NSString FIREmailPasswordAuthSignInMethod { get; }

		// extern NS_SWIFT_NAME(FacebookAuthProviderID) NSString *const FIRFacebookAuthProviderID __attribute__((swift_name("FacebookAuthProviderID")));
		[Field ("FIRFacebookAuthProviderID", "__Internal")]
		NSString FIRFacebookAuthProviderID { get; }

		// extern NS_SWIFT_NAME(FacebookAuthSignInMethod) NSString *const FIRFacebookAuthSignInMethod __attribute__((swift_name("FacebookAuthSignInMethod")));
		[Field ("FIRFacebookAuthSignInMethod", "__Internal")]
		NSString FIRFacebookAuthSignInMethod { get; }
	}

	// typedef void (^FIRAuthCredentialCallback)(FIRAuthCredential * _Nullable, NSError * _Nullable);
	delegate void FIRAuthCredentialCallback ([NullAllowed] FIRAuthCredential arg0, [NullAllowed] NSError arg1);

	partial interface Constants {
		// extern NS_SWIFT_NAME(GameCenterAuthProviderID) NSString *const FIRGameCenterAuthProviderID __attribute__((swift_name("GameCenterAuthProviderID")));
		[Field ("FIRGameCenterAuthProviderID", "__Internal")]
		NSString FIRGameCenterAuthProviderID { get; }

		// extern NS_SWIFT_NAME(
// GameCenterAuthSignInMethod) NSString *const FIRGameCenterAuthSignInMethod __attribute__((swift_name("GameCenterAuthSignInMethod")));
		[Field ("FIRGameCenterAuthSignInMethod", "__Internal")]
		NSString FIRGameCenterAuthSignInMethod { get; }
	}

	// typedef void (^FIRGameCenterCredentialCallback)(FIRAuthCredential * _Nullable, NSError * _Nullable);
	delegate void FIRGameCenterCredentialCallback ([NullAllowed] FIRAuthCredential arg0, [NullAllowed] NSError arg1);

	partial interface Constants {
		// extern NS_SWIFT_NAME(GitHubAuthProviderID) NSString *const FIRGitHubAuthProviderID __attribute__((swift_name("GitHubAuthProviderID")));
		[Field ("FIRGitHubAuthProviderID", "__Internal")]
		NSString FIRGitHubAuthProviderID { get; }

		// extern NS_SWIFT_NAME(GitHubAuthSignInMethod) NSString *const FIRGitHubAuthSignInMethod __attribute__((swift_name("GitHubAuthSignInMethod")));
		[Field ("FIRGitHubAuthSignInMethod", "__Internal")]
		NSString FIRGitHubAuthSignInMethod { get; }

		// extern NS_SWIFT_NAME(GoogleAuthProviderID) NSString *const FIRGoogleAuthProviderID __attribute__((swift_name("GoogleAuthProviderID")));
		[Field ("FIRGoogleAuthProviderID", "__Internal")]
		NSString FIRGoogleAuthProviderID { get; }

		// extern NS_SWIFT_NAME(GoogleAuthSignInMethod) NSString *const FIRGoogleAuthSignInMethod __attribute__((swift_name("GoogleAuthSignInMethod")));
		[Field ("FIRGoogleAuthSignInMethod", "__Internal")]
		NSString FIRGoogleAuthSignInMethod { get; }
	}

	// typedef void (^FIRMultiFactorSessionCallback)(FIRMultiFactorSession * _Nullable, NSError * _Nullable);
	delegate void FIRMultiFactorSessionCallback ([NullAllowed] FIRMultiFactorSession arg0, [NullAllowed] NSError arg1);

	partial interface Constants {
		// extern NS_SWIFT_NAME(PhoneMultiFactorID) NSString *const FIRPhoneMultiFactorID __attribute__((swift_name("PhoneMultiFactorID"))) __attribute__((availability(tvos, unavailable))) __attribute__((availability(watchos, unavailable)));
		[NoTV]
		[Field ("FIRPhoneMultiFactorID", "__Internal")]
		NSString FIRPhoneMultiFactorID { get; }

		// extern NS_SWIFT_NAME(TOTPMultiFactorID) NSString *const FIRTOTPMultiFactorID __attribute__((swift_name("TOTPMultiFactorID"))) __attribute__((availability(tvos, unavailable))) __attribute__((availability(watchos, unavailable)));
		[NoTV]
		[Field ("FIRTOTPMultiFactorID", "__Internal")]
		NSString FIRTOTPMultiFactorID { get; }

		// extern NS_SWIFT_NAME(PhoneAuthProviderID) NSString *const FIRPhoneAuthProviderID __attribute__((swift_name("PhoneAuthProviderID"))) __attribute__((availability(macos, unavailable))) __attribute__((availability(tvos, unavailable))) __attribute__((availability(watchos, unavailable)));
		[NoTV, NoMac]
		[Field ("FIRPhoneAuthProviderID", "__Internal")]
		NSString FIRPhoneAuthProviderID { get; }

		// extern NS_SWIFT_NAME(PhoneAuthSignInMethod) NSString *const FIRPhoneAuthSignInMethod __attribute__((swift_name("PhoneAuthSignInMethod"))) __attribute__((availability(macos, unavailable))) __attribute__((availability(tvos, unavailable))) __attribute__((availability(watchos, unavailable)));
		[NoTV, NoMac]
		[Field ("FIRPhoneAuthSignInMethod", "__Internal")]
		NSString FIRPhoneAuthSignInMethod { get; }
	}

	// typedef void (^FIRVerificationResultCallback)(NSString * _Nullable, NSError * _Nullable);
	delegate void FIRVerificationResultCallback ([NullAllowed] string arg0, [NullAllowed] NSError arg1);

	partial interface Constants {
		// extern NS_SWIFT_NAME(TwitterAuthProviderID) NSString *const FIRTwitterAuthProviderID __attribute__((swift_name("TwitterAuthProviderID")));
		[Field ("FIRTwitterAuthProviderID", "__Internal")]
		NSString FIRTwitterAuthProviderID { get; }

		// extern NS_SWIFT_NAME(TwitterAuthSignInMethod) NSString *const FIRTwitterAuthSignInMethod __attribute__((swift_name("TwitterAuthSignInMethod")));
		[Field ("FIRTwitterAuthSignInMethod", "__Internal")]
		NSString FIRTwitterAuthSignInMethod { get; }
	}

	// typedef void (^FIRAuthTokenCallback)(NSString * _Nullable, NSError * _Nullable);
	delegate void FIRAuthTokenCallback ([NullAllowed] string arg0, [NullAllowed] NSError arg1);

	// typedef void (^FIRAuthTokenResultCallback)(FIRAuthTokenResult * _Nullable, NSError * _Nullable);
	delegate void FIRAuthTokenResultCallback ([NullAllowed] FIRAuthTokenResult arg0, [NullAllowed] NSError arg1);

	// typedef void (^FIRUserProfileChangeCallback)(NSError * _Nullable);
	delegate void FIRUserProfileChangeCallback ([NullAllowed] NSError arg0);

	// typedef void (^FIRSendEmailVerificationCallback)(NSError * _Nullable);
	delegate void FIRSendEmailVerificationCallback ([NullAllowed] NSError arg0);

	partial interface Constants {
		// extern double FirebaseAuthVersionNumber;
		[Field ("FirebaseAuthVersionNumber", "__Internal")]
		double FirebaseAuthVersionNumber { get; }// 
// 
// 		// extern const unsigned char[] FirebaseAuthVersionString;
// 		[Field ("FirebaseAuthVersionString", "__Internal")]
// 		byte [] FirebaseAuthVersionString { get; }
// 
	}

	// @interface FIRActionCodeInfo : NSObject
	[BaseType (typeof (NSObject))]
	[DisableDefaultCtor]
	interface FIRActionCodeInfo {
		// @property (readonly, nonatomic) enum FIRActionCodeOperation operation;
		[Export ("operation")]
		FIRActionCodeOperation Operation { get; }

		// @property (readonly, copy, nonatomic) NSString * _Nonnull email;
		[Export ("email")]
		string Email { get; }

		// @property (readonly, copy, nonatomic) NSString * _Nullable previousEmail;
		[NullAllowed, Export ("previousEmail")]
		string PreviousEmail { get; }
	}

	// @interface FIRActionCodeSettings : NSObject
	[BaseType (typeof (NSObject))]
	interface FIRActionCodeSettings {
		// @property (copy, nonatomic) NSURL * _Nullable URL;
		[NullAllowed, Export ("URL", ArgumentSemantic.Copy)]
		NSUrl URL { get; set; }

		// @property (nonatomic) BOOL handleCodeInApp;
		[Export ("handleCodeInApp")]
		bool HandleCodeInApp { get; set; }

		// @property (copy, nonatomic) NSString * _Nullable iOSBundleID;
		[NullAllowed, Export ("iOSBundleID")]
		string IOSBundleID { get; set; }

		// @property (copy, nonatomic) NSString * _Nullable androidPackageName;
		[NullAllowed, Export ("androidPackageName")]
		string AndroidPackageName { get; set; }

		// @property (copy, nonatomic) NSString * _Nullable androidMinimumVersion;
		[NullAllowed, Export ("androidMinimumVersion")]
		string AndroidMinimumVersion { get; set; }

		// @property (nonatomic) BOOL androidInstallIfNotAvailable;
		[Export ("androidInstallIfNotAvailable")]
		bool AndroidInstallIfNotAvailable { get; set; }

		// @property (copy, nonatomic) NSString * _Nullable linkDomain;
		[NullAllowed, Export ("linkDomain")]
		string LinkDomain { get; set; }

		// -(void)setAndroidPackageName:(NSString * _Nonnull)androidPackageName installIfNotAvailable:(BOOL)installIfNotAvailable minimumVersion:(NSString * _Nullable)minimumVersion;
		[Export ("setAndroidPackageName:installIfNotAvailable:minimumVersion:")]
		void SetAndroidPackageName (string androidPackageName, bool installIfNotAvailable, [NullAllowed] string minimumVersion);
	}

	// @interface FIRActionCodeURL : NSObject
	[BaseType (typeof (NSObject))]
	[DisableDefaultCtor]
	interface FIRActionCodeURL {
		// @property (readonly, copy, nonatomic) NSString * _Nullable APIKey;
		[NullAllowed, Export ("APIKey")]
		string APIKey { get; }

		// @property (readonly, nonatomic) enum FIRActionCodeOperation operation;
		[Export ("operation")]
		FIRActionCodeOperation Operation { get; }

		// @property (readonly, copy, nonatomic) NSString * _Nullable code;
		[NullAllowed, Export ("code")]
		string Code { get; }

		// @property (readonly, copy, nonatomic) NSURL * _Nullable continueURL;
		[NullAllowed, Export ("continueURL", ArgumentSemantic.Copy)]
		NSUrl ContinueURL { get; }

		// @property (readonly, copy, nonatomic) NSString * _Nullable languageCode;
		[NullAllowed, Export ("languageCode")]
		string LanguageCode { get; }

		// -(instancetype _Nullable)actionCodeURLWithLink:(NSString * _Nonnull)link __attribute__((objc_designated_initializer)) __attribute__((objc_method_family("init")));
		[Export ("actionCodeURLWithLink:")]
		[DesignatedInitializer]
		NativeHandle Constructor (string link);
	}

	// @interface FIRAdditionalUserInfo : NSObject
	[BaseType (typeof (NSObject))]
	[DisableDefaultCtor]
	interface FIRAdditionalUserInfo {
		// @property (readonly, copy, nonatomic) NSString * _Nonnull providerID;
		[Export ("providerID")]
		string ProviderID { get; }

		// @property (readonly, copy, nonatomic) NSDictionary<NSString *,id> * _Nullable profile;
		[NullAllowed, Export ("profile", ArgumentSemantic.Copy)]
		NSDictionary<NSString, NSObject> Profile { get; }

		// @property (readonly, copy, nonatomic) NSString * _Nullable username;
		[NullAllowed, Export ("username")]
		string Username { get; }

		// @property (readonly, nonatomic) BOOL isNewUser;
		[Export ("isNewUser")]
		bool IsNewUser { get; }

		// -(BOOL)newUser __attribute__((warn_unused_result("")));
		[Export ("newUser")]
		bool NewUser { get; }

		// @property (readonly, nonatomic, class) BOOL supportsSecureCoding;
		[Static]
		[Export ("supportsSecureCoding")]
		bool SupportsSecureCoding { get; }

		// -(instancetype _Nullable)initWithCoder:(NSCoder * _Nonnull)aDecoder __attribute__((objc_designated_initializer));
		[Export ("initWithCoder:")]
		[DesignatedInitializer]
		NativeHandle Constructor (NSCoder aDecoder);

		// -(void)encodeWithCoder:(NSCoder * _Nonnull)aCoder;
		[Export ("encodeWithCoder:")]
		void EncodeWithCoder (NSCoder aCoder);
	}
	// @interface FIRAuth : NSObject
	[TV (13, 0), MacCatalyst (13, 0), Mac (10, 15), iOS (13, 0)]
	[BaseType (typeof (NSObject))]
	[DisableDefaultCtor]
	interface FIRAuth {
		// +(FIRAuth * _Nonnull)auth __attribute__((warn_unused_result("")));
		[Static]
		[Export ("auth")]
		FIRAuth Auth { get; }

		// +(FIRAuth * _Nonnull)authWithApp:(FIRApp * _Nonnull)app __attribute__((warn_unused_result("")));
		[Static]
		[Export ("authWithApp:")]
		FIRAuth AuthWithApp (FIRApp app);

		// @property (readonly, nonatomic, weak) FIRApp * _Nullable app;
		[NullAllowed, Export ("app", ArgumentSemantic.Weak)]
		FIRApp App { get; }

		// @property (readonly, nonatomic, strong) FIRUser * _Nullable currentUser;
		[NullAllowed, Export ("currentUser", ArgumentSemantic.Strong)]
		FIRUser CurrentUser { get; }

		// @property (copy, nonatomic) NSString * _Nullable languageCode;
		[NullAllowed, Export ("languageCode")]
		string LanguageCode { get; set; }

		// @property (nonatomic, strong) FIRAuthSettings * _Nullable settings;
		[NullAllowed, Export ("settings", ArgumentSemantic.Strong)]
		FIRAuthSettings Settings { get; set; }

		// @property (readonly, copy, nonatomic) NSString * _Nullable userAccessGroup;
		[NullAllowed, Export ("userAccessGroup")]
		string UserAccessGroup { get; }

		// @property (nonatomic) BOOL shareAuthStateAcrossDevices;
		[Export ("shareAuthStateAcrossDevices")]
		bool ShareAuthStateAcrossDevices { get; set; }

		// @property (copy, nonatomic) NSString * _Nullable tenantID;
		[NullAllowed, Export ("tenantID")]
		string TenantID { get; set; }

		// @property (copy, nonatomic) NSString * _Nullable customAuthDomain;
		[NullAllowed, Export ("customAuthDomain")]
		string CustomAuthDomain { get; set; }

		// -(void)updateCurrentUser:(FIRUser * _Nullable)user completion:(void (^ _Nullable)(NSError * _Nullable))completion;
		[Export ("updateCurrentUser:completion:")]
		void UpdateCurrentUser ([NullAllowed] FIRUser user, [NullAllowed] Action<NSError> completion);

		// -(void)fetchSignInMethodsForEmail:(NSString * _Nonnull)email completion:(void (^ _Nullable)(NSArray<NSString *> * _Nullable, NSError * _Nullable))completion __attribute__((deprecated("`fetchSignInMethods` is deprecated and will be removed in a future release. This method returns an empty list when Email Enumeration Protection is enabled.")));
		[Export ("fetchSignInMethodsForEmail:completion:")]
		void FetchSignInMethodsForEmail (string email, [NullAllowed] Action<NSArray<NSString>, NSError> completion);

		// -(void)signInWithEmail:(NSString * _Nonnull)email password:(NSString * _Nonnull)password completion:(void (^ _Nullable)(FIRAuthDataResult * _Nullable, NSError * _Nullable))completion;
		[Export ("signInWithEmail:password:completion:")]
		void SignInWithEmail (string email, string password, [NullAllowed] Action<FIRAuthDataResult, NSError> completion);// 
// 
// 		// -(void)signInWithEmail:(NSString * _Nonnull)email link:(NSString * _Nonnull)link completion:(void (^ _Nullable)(FIRAuthDataResult * _Nullable, NSError * _Nullable))completion;
// 		[Export ("signInWithEmail:link:completion:")]
// 		void SignInWithEmail (string email, string link, [NullAllowed] Action<FIRAuthDataResult, NSError> completion);
// 

		// -(void)signInWithCredential:(FIRAuthCredential * _Nonnull)credential completion:(void (^ _Nullable)(FIRAuthDataResult * _Nullable, NSError * _Nullable))completion;
		[Export ("signInWithCredential:completion:")]
		void SignInWithCredential (FIRAuthCredential credential, [NullAllowed] Action<FIRAuthDataResult, NSError> completion);

		// -(void)signInAnonymouslyWithCompletion:(void (^ _Nullable)(FIRAuthDataResult * _Nullable, NSError * _Nullable))completion;
		[Export ("signInAnonymouslyWithCompletion:")]
		void SignInAnonymouslyWithCompletion ([NullAllowed] Action<FIRAuthDataResult, NSError> completion);

		// -(void)signInAnonymouslyWithCompletionHandler:(void (^ _Nonnull)(FIRAuthDataResult * _Nullable, NSError * _Nullable))completionHandler __attribute__((availability(watchos, introduced=7))) __attribute__((availability(maccatalyst, introduced=13))) __attribute__((availability(macos, introduced=10.15))) __attribute__((availability(tvos, introduced=13))) __attribute__((availability(ios, introduced=13)));
		[TV (13, 0), MacCatalyst (13, 0), Mac (10, 15), iOS (13, 0)]
		[Export ("signInAnonymouslyWithCompletionHandler:")]
		void SignInAnonymouslyWithCompletionHandler (Action<FIRAuthDataResult, NSError> completionHandler);

		// -(void)signInWithCustomToken:(NSString * _Nonnull)token completion:(void (^ _Nullable)(FIRAuthDataResult * _Nullable, NSError * _Nullable))completion;
		[Export ("signInWithCustomToken:completion:")]
		void SignInWithCustomToken (string token, [NullAllowed] Action<FIRAuthDataResult, NSError> completion);

		// -(void)createUserWithEmail:(NSString * _Nonnull)email password:(NSString * _Nonnull)password completion:(void (^ _Nullable)(FIRAuthDataResult * _Nullable, NSError * _Nullable))completion;
		[Export ("createUserWithEmail:password:completion:")]
		void CreateUserWithEmail (string email, string password, [NullAllowed] Action<FIRAuthDataResult, NSError> completion);

		// -(void)confirmPasswordResetWithCode:(NSString * _Nonnull)code newPassword:(NSString * _Nonnull)newPassword completion:(void (^ _Nonnull)(NSError * _Nullable))completion;
		[Export ("confirmPasswordResetWithCode:newPassword:completion:")]
		void ConfirmPasswordResetWithCode (string code, string newPassword, Action<NSError> completion);

		// -(void)checkActionCode:(NSString * _Nonnull)code completion:(void (^ _Nonnull)(FIRActionCodeInfo * _Nullable, NSError * _Nullable))completion;
		[Export ("checkActionCode:completion:")]
		void CheckActionCode (string code, Action<FIRActionCodeInfo, NSError> completion);

		// -(void)verifyPasswordResetCode:(NSString * _Nonnull)code completion:(void (^ _Nonnull)(NSString * _Nullable, NSError * _Nullable))completion;
		[Export ("verifyPasswordResetCode:completion:")]
		void VerifyPasswordResetCode (string code, Action<NSString, NSError> completion);

		// -(void)applyActionCode:(NSString * _Nonnull)code completion:(void (^ _Nonnull)(NSError * _Nullable))completion;
		[Export ("applyActionCode:completion:")]
		void ApplyActionCode (string code, Action<NSError> completion);

		// -(void)sendPasswordResetWithEmail:(NSString * _Nonnull)email completion:(void (^ _Nullable)(NSError * _Nullable))completion;
		[Export ("sendPasswordResetWithEmail:completion:")]
		void SendPasswordResetWithEmail (string email, [NullAllowed] Action<NSError> completion);

		// -(void)sendPasswordResetWithEmail:(NSString * _Nonnull)email actionCodeSettings:(FIRActionCodeSettings * _Nullable)actionCodeSettings completion:(void (^ _Nullable)(NSError * _Nullable))completion;
		[Export ("sendPasswordResetWithEmail:actionCodeSettings:completion:")]
		void SendPasswordResetWithEmail (string email, [NullAllowed] FIRActionCodeSettings actionCodeSettings, [NullAllowed] Action<NSError> completion);

		// -(void)sendSignInLinkToEmail:(NSString * _Nonnull)email actionCodeSettings:(FIRActionCodeSettings * _Nonnull)actionCodeSettings completion:(void (^ _Nullable)(NSError * _Nullable))completion;
		[Export ("sendSignInLinkToEmail:actionCodeSettings:completion:")]
		void SendSignInLinkToEmail (string email, FIRActionCodeSettings actionCodeSettings, [NullAllowed] Action<NSError> completion);

		// -(BOOL)signOut:(NSError * _Nullable * _Nullable)error;
		[Export ("signOut:")]
		bool SignOut ([NullAllowed] out NSError error);

		// -(BOOL)isSignInWithEmailLink:(NSString * _Nonnull)link __attribute__((warn_unused_result("")));
		[Export ("isSignInWithEmailLink:")]
		bool IsSignInWithEmailLink (string link);

		// -(id<NSObject> _Nonnull)addAuthStateDidChangeListener:(void (^ _Nonnull)(FIRAuth * _Nonnull, FIRUser * _Nullable))listener __attribute__((warn_unused_result("")));
		[Export ("addAuthStateDidChangeListener:")]
		NSObject AddAuthStateDidChangeListener (Action<FIRAuth, FIRUser> listener);

		// -(void)removeAuthStateDidChangeListener:(id<NSObject> _Nonnull)listenerHandle;
		[Export ("removeAuthStateDidChangeListener:")]
		void RemoveAuthStateDidChangeListener (NSObject listenerHandle);

		// -(id<NSObject> _Nonnull)addIDTokenDidChangeListener:(void (^ _Nonnull)(FIRAuth * _Nonnull, FIRUser * _Nullable))listener __attribute__((warn_unused_result("")));
		[Export ("addIDTokenDidChangeListener:")]
		NSObject AddIDTokenDidChangeListener (Action<FIRAuth, FIRUser> listener);

		// -(void)removeIDTokenDidChangeListener:(id<NSObject> _Nonnull)listenerHandle;
		[Export ("removeIDTokenDidChangeListener:")]
		void RemoveIDTokenDidChangeListener (NSObject listenerHandle);

		// -(void)useAppLanguage;
		[Export ("useAppLanguage")]
		void UseAppLanguage ();

		// -(void)useEmulatorWithHost:(NSString * _Nonnull)host port:(NSInteger)port;
		[Export ("useEmulatorWithHost:port:")]
		void UseEmulatorWithHost (string host, nint port);

		// -(void)revokeTokenWithAuthorizationCode:(NSString * _Nonnull)authorizationCode completion:(void (^ _Nullable)(NSError * _Nullable))completion;
		[Export ("revokeTokenWithAuthorizationCode:completion:")]
		void RevokeTokenWithAuthorizationCode (string authorizationCode, [NullAllowed] Action<NSError> completion);

		// -(BOOL)useUserAccessGroup:(NSString * _Nullable)accessGroup error:(NSError * _Nullable * _Nullable)error;
		[Export ("useUserAccessGroup:error:")]
		bool UseUserAccessGroup ([NullAllowed] string accessGroup, [NullAllowed] out NSError error);

		// -(FIRUser * _Nullable)getStoredUserForAccessGroup:(NSString * _Nullable)accessGroup error:(NSError * _Nullable * _Nullable)outError __attribute__((warn_unused_result("")));
		[Export ("getStoredUserForAccessGroup:error:")]
		[return: NullAllowed]
		FIRUser GetStoredUserForAccessGroup ([NullAllowed] string accessGroup, [NullAllowed] out NSError outError);
	}
// 
// 	// @interface FirebaseAuth_Swift_922 (FIRAuth) <FIRAuthInterop>
// 	[TV (13, 0), MacCatalyst (13, 0), Mac (10, 15), iOS (13, 0)]
// 	[Category]
// 	[BaseType (typeof (FIRAuth))]
// 	interface FIRAuth_FirebaseAuth_Swift_922 : IFIRAuthInterop {
// 		// -(void)getTokenForcingRefresh:(BOOL)forceRefresh withCallback:(void (^ _Nonnull)(NSString * _Nullable, NSError * _Nullable))callback __attribute__((swift_name("getToken(forcingRefresh:completion:)")));
// 		[Export ("getTokenForcingRefresh:withCallback:")]
// 		void GetTokenForcingRefresh (bool forceRefresh, Action<NSString, NSError> callback);
// 
// 		// -(NSString * _Nullable)getUserID __attribute__((warn_unused_result("")));
// 		[NullAllowed, Export ("getUserID")]
// 		string UserID { get; }
// 	}

	// @interface FIRAuthCredential : NSObject
	[TV (13, 0), MacCatalyst (13, 0), Mac (10, 15), iOS (13, 0)]
	[BaseType (typeof (NSObject))]
	[DisableDefaultCtor]
	interface FIRAuthCredential {
		// @property (readonly, copy, nonatomic) NSString * _Nonnull provider;
		[Export ("provider")]
		string Provider { get; }
	}

	// @interface FIRAuthDataResult : NSObject
	[TV (13, 0), MacCatalyst (13, 0), Mac (10, 15), iOS (13, 0)]
	[BaseType (typeof (NSObject))]
	[DisableDefaultCtor]
	interface FIRAuthDataResult {
		// @property (readonly, nonatomic, strong) FIRUser * _Nonnull user;
		[Export ("user", ArgumentSemantic.Strong)]
		FIRUser User { get; }

		// @property (readonly, nonatomic, strong) FIRAdditionalUserInfo * _Nullable additionalUserInfo;
		[NullAllowed, Export ("additionalUserInfo", ArgumentSemantic.Strong)]
		FIRAdditionalUserInfo AdditionalUserInfo { get; }// 
// 
// 		// @property (readonly, nonatomic, strong) FIROAuthCredential * _Nullable credential;
// 		[NullAllowed, Export ("credential", ArgumentSemantic.Strong)]
// 		FIROAuthCredential Credential { get; }
// 
// 
		// @property (readonly, nonatomic, class) BOOL supportsSecureCoding;
		[Static]
		[Export ("supportsSecureCoding")]
		bool SupportsSecureCoding { get; }

		// -(void)encodeWithCoder:(NSCoder * _Nonnull)coder;
		[Export ("encodeWithCoder:")]
		void EncodeWithCoder (NSCoder coder);

		// -(instancetype _Nullable)initWithCoder:(NSCoder * _Nonnull)coder __attribute__((objc_designated_initializer));
		[Export ("initWithCoder:")]
		[DesignatedInitializer]
		NativeHandle Constructor (NSCoder coder);
	}

	// @interface FirebaseAuth_Swift_964 (FIRAuthDataResult) <NSSecureCoding>
	[TV (13, 0), MacCatalyst (13, 0), Mac (10, 15), iOS (13, 0)]
	partial interface Constants {
		// NSString *const _Nonnull FIRAuthErrorCodeDomain;
		[Field ("FIRAuthErrorCodeDomain", "__Internal")]
		NSString FIRAuthErrorCodeDomain { get; }
	}

	// @interface FIRAuthErrors : NSObject
	[BaseType (typeof (NSObject))]
	interface FIRAuthErrors {
		// @property (readonly, copy, nonatomic, class) NSString * _Nonnull domain;
		[Static]
		[Export ("domain")]
		string Domain { get; }

		// @property (readonly, copy, nonatomic, class) NSString * _Nonnull userInfoNameKey;
		[Static]
		[Export ("userInfoNameKey")]
		string UserInfoNameKey { get; }

		// @property (readonly, copy, nonatomic, class) NSString * _Nonnull userInfoEmailKey;
		[Static]
		[Export ("userInfoEmailKey")]
		string UserInfoEmailKey { get; }

		// @property (readonly, copy, nonatomic, class) NSString * _Nonnull userInfoUpdatedCredentialKey;
		[Static]
		[Export ("userInfoUpdatedCredentialKey")]
		string UserInfoUpdatedCredentialKey { get; }

		// @property (readonly, copy, nonatomic, class) NSString * _Nonnull FIRAuthErrorUserInfoMultiFactorResolverKey;
		[Static]
		[Export ("FIRAuthErrorUserInfoMultiFactorResolverKey")]
		string FIRAuthErrorUserInfoMultiFactorResolverKey { get; }
	}

	// @interface FIRAuthSettings : NSObject <NSCopying>
	[BaseType (typeof (NSObject))]
	[DisableDefaultCtor]
	interface FIRAuthSettings : INSCopying {
		// @property (nonatomic) BOOL appVerificationDisabledForTesting;
		[Export ("appVerificationDisabledForTesting")]
		bool AppVerificationDisabledForTesting { get; set; }

		// @property (nonatomic) BOOL isAppVerificationDisabledForTesting;
		[Export ("isAppVerificationDisabledForTesting")]
		bool IsAppVerificationDisabledForTesting { get; set; }// 
// 
// 		// -(id _Nonnull)copyWithZone:(struct _NSZone * _Nullable)zone __attribute__((warn_unused_result("")));
// 		[Export ("copyWithZone:")]
// 		unsafe NSObject CopyWithZone ([NullAllowed] _NSZone* zone);
// 
	}

	// @interface FIRAuthTokenResult : NSObject
	[TV (13, 0), MacCatalyst (13, 0), Mac (10, 15), iOS (13, 0)]
	[BaseType (typeof (NSObject))]
	[DisableDefaultCtor]
	interface FIRAuthTokenResult {
		// @property (copy, nonatomic) NSString * _Nonnull token;
		[Export ("token")]
		string Token { get; set; }

		// @property (copy, nonatomic) NSDate * _Nonnull expirationDate;
		[Export ("expirationDate", ArgumentSemantic.Copy)]
		NSDate ExpirationDate { get; set; }

		// @property (copy, nonatomic) NSDate * _Nonnull authDate;
		[Export ("authDate", ArgumentSemantic.Copy)]
		NSDate AuthDate { get; set; }

		// @property (copy, nonatomic) NSDate * _Nonnull issuedAtDate;
		[Export ("issuedAtDate", ArgumentSemantic.Copy)]
		NSDate IssuedAtDate { get; set; }

		// @property (copy, nonatomic) NSString * _Nonnull signInProvider;
		[Export ("signInProvider")]
		string SignInProvider { get; set; }

		// @property (copy, nonatomic) NSString * _Nonnull signInSecondFactor;
		[Export ("signInSecondFactor")]
		string SignInSecondFactor { get; set; }

		// @property (copy, nonatomic) NSDictionary<NSString *,id> * _Nonnull claims;
		[Export ("claims", ArgumentSemantic.Copy)]
		NSDictionary<NSString, NSObject> Claims { get; set; }

		// @property (readonly, nonatomic, class) BOOL supportsSecureCoding;
		[Static]
		[Export ("supportsSecureCoding")]
		bool SupportsSecureCoding { get; }

		// -(void)encodeWithCoder:(NSCoder * _Nonnull)coder;
		[Export ("encodeWithCoder:")]
		void EncodeWithCoder (NSCoder coder);

		// -(instancetype _Nullable)initWithCoder:(NSCoder * _Nonnull)coder;
		[Export ("initWithCoder:")]
		NativeHandle Constructor (NSCoder coder);
	}
	// @interface FIREmailAuthProvider : NSObject
	[TV (13, 0), MacCatalyst (13, 0), Mac (10, 15), iOS (13, 0)]
	[BaseType (typeof (NSObject))]
	interface FIREmailAuthProvider {
		// @property (readonly, copy, nonatomic, class) NSString * _Nonnull id;
		[Static]
		[Export ("id")]
		string Id { get; }

		// +(FIRAuthCredential * _Nonnull)credentialWithEmail:(NSString * _Nonnull)email password:(NSString * _Nonnull)password __attribute__((warn_unused_result("")));
		[Static]
		[Export ("credentialWithEmail:password:")]
		FIRAuthCredential CredentialWithEmail (string email, string password);// 
// 
// 		// +(FIRAuthCredential * _Nonnull)credentialWithEmail:(NSString * _Nonnull)email link:(NSString * _Nonnull)link __attribute__((warn_unused_result("")));
// 		[Static]
// 		[Export ("credentialWithEmail:link:")]
// 		FIRAuthCredential CredentialWithEmail (string email, string link);
// 
	}

	// @interface FIRFacebookAuthProvider : NSObject
	[TV (13, 0), MacCatalyst (13, 0), Mac (10, 15), iOS (13, 0)]
	[BaseType (typeof (NSObject))]
	[DisableDefaultCtor]
	interface FIRFacebookAuthProvider {
		// @property (readonly, copy, nonatomic, class) NSString * _Nonnull id;
		[Static]
		[Export ("id")]
		string Id { get; }

		// +(FIRAuthCredential * _Nonnull)credentialWithAccessToken:(NSString * _Nonnull)accessToken __attribute__((warn_unused_result("")));
		[Static]
		[Export ("credentialWithAccessToken:")]
		FIRAuthCredential CredentialWithAccessToken (string accessToken);
	}

	// @protocol FIRFederatedAuthProvider <NSObject>
	/*
  Check whether adding [Model] to this declaration is appropriate.
  [Model] is used to generate a C# class that implements this protocol,
  and might be useful for protocols that consumers are supposed to implement,
  since consumers can subclass the generated class instead of implementing
  the generated interface. If consumers are not supposed to implement this
  protocol, then [Model] is redundant and will generate code that will never
  be used.
*/
	[TV (13, 0), MacCatalyst (13, 0), Mac (10, 15), iOS (13, 0)]
	[Protocol]
	[BaseType (typeof (NSObject))]
	interface FIRFederatedAuthProvider {
	}

	// @interface FIRGameCenterAuthProvider : NSObject
	[TV (13, 0), MacCatalyst (13, 0), Mac (10, 15), iOS (13, 0)]
	[BaseType (typeof (NSObject))]
	[DisableDefaultCtor]
	interface FIRGameCenterAuthProvider {
		// @property (readonly, copy, nonatomic, class) NSString * _Nonnull id;
		[Static]
		[Export ("id")]
		string Id { get; }

		// +(void)getCredentialWithCompletion:(void (^ _Nonnull)(FIRAuthCredential * _Nullable, NSError * _Nullable))completion;
		[Static]
		[Export ("getCredentialWithCompletion:")]
		void GetCredentialWithCompletion (Action<FIRAuthCredential, NSError> completion);
	}

	// @interface FIRGitHubAuthProvider : NSObject
	[TV (13, 0), MacCatalyst (13, 0), Mac (10, 15), iOS (13, 0)]
	[BaseType (typeof (NSObject))]
	[DisableDefaultCtor]
	interface FIRGitHubAuthProvider {
		// @property (readonly, copy, nonatomic, class) NSString * _Nonnull id;
		[Static]
		[Export ("id")]
		string Id { get; }

		// +(FIRAuthCredential * _Nonnull)credentialWithToken:(NSString * _Nonnull)token __attribute__((warn_unused_result("")));
		[Static]
		[Export ("credentialWithToken:")]
		FIRAuthCredential CredentialWithToken (string token);
	}

	// @interface FIRGoogleAuthProvider : NSObject
	[TV (13, 0), MacCatalyst (13, 0), Mac (10, 15), iOS (13, 0)]
	[BaseType (typeof (NSObject))]
	[DisableDefaultCtor]
	interface FIRGoogleAuthProvider {
		// @property (readonly, copy, nonatomic, class) NSString * _Nonnull id;
		[Static]
		[Export ("id")]
		string Id { get; }

		// +(FIRAuthCredential * _Nonnull)credentialWithIDToken:(NSString * _Nonnull)idToken accessToken:(NSString * _Nonnull)accessToken __attribute__((warn_unused_result("")));
		[Static]
		[Export ("credentialWithIDToken:accessToken:")]
		FIRAuthCredential CredentialWithIDToken (string idToken, string accessToken);
	}

	// @interface FIRMultiFactor : NSObject
	[TV (13, 0), MacCatalyst (13, 0), Mac (10, 15), iOS (13, 0)]
	[BaseType (typeof (NSObject))]
	[DisableDefaultCtor]
	interface FIRMultiFactor {
		// @property (copy, nonatomic) NSArray<FIRMultiFactorInfo *> * _Nonnull enrolledFactors;
		[Export ("enrolledFactors", ArgumentSemantic.Copy)]
		FIRMultiFactorInfo [] EnrolledFactors { get; set; }

		// -(void)getSessionWithCompletion:(void (^ _Nullable)(FIRMultiFactorSession * _Nullable, NSError * _Nullable))completion;
		[Export ("getSessionWithCompletion:")]
		void GetSessionWithCompletion ([NullAllowed] Action<FIRMultiFactorSession, NSError> completion);

		// -(void)enrollWithAssertion:(FIRMultiFactorAssertion * _Nonnull)assertion displayName:(NSString * _Nullable)displayName completion:(void (^ _Nullable)(NSError * _Nullable))completion;
		[Export ("enrollWithAssertion:displayName:completion:")]
		void EnrollWithAssertion (FIRMultiFactorAssertion assertion, [NullAllowed] string displayName, [NullAllowed] Action<NSError> completion);

		// -(void)unenrollWithInfo:(FIRMultiFactorInfo * _Nonnull)factorInfo completion:(void (^ _Nullable)(NSError * _Nullable))completion;
		[Export ("unenrollWithInfo:completion:")]
		void UnenrollWithInfo (FIRMultiFactorInfo factorInfo, [NullAllowed] Action<NSError> completion);

		// -(void)unenrollWithFactorUID:(NSString * _Nonnull)factorUID completion:(void (^ _Nullable)(NSError * _Nullable))completion;
		[Export ("unenrollWithFactorUID:completion:")]
		void UnenrollWithFactorUID (string factorUID, [NullAllowed] Action<NSError> completion);

		// @property (readonly, nonatomic, class) BOOL supportsSecureCoding;
		[Static]
		[Export ("supportsSecureCoding")]
		bool SupportsSecureCoding { get; }

		// -(void)encodeWithCoder:(NSCoder * _Nonnull)coder;
		[Export ("encodeWithCoder:")]
		void EncodeWithCoder (NSCoder coder);

		// -(instancetype _Nullable)initWithCoder:(NSCoder * _Nonnull)coder __attribute__((objc_designated_initializer));
		[Export ("initWithCoder:")]
		[DesignatedInitializer]
		NativeHandle Constructor (NSCoder coder);
	}
	// @interface FIRMultiFactorAssertion : NSObject
	[BaseType (typeof (NSObject))]
	[DisableDefaultCtor]
	interface FIRMultiFactorAssertion {
		// @property (copy, nonatomic) NSString * _Nonnull factorID;
		[Export ("factorID")]
		string FactorID { get; set; }
	}

	// @interface FIRMultiFactorInfo : NSObject
	[BaseType (typeof (NSObject))]
	[DisableDefaultCtor]
	interface FIRMultiFactorInfo {
		// @property (readonly, copy, nonatomic) NSString * _Nonnull UID;
		[Export ("UID")]
		string UID { get; }

		// @property (readonly, copy, nonatomic) NSString * _Nullable displayName;
		[NullAllowed, Export ("displayName")]
		string DisplayName { get; }

		// @property (readonly, copy, nonatomic) NSDate * _Nonnull enrollmentDate;
		[Export ("enrollmentDate", ArgumentSemantic.Copy)]
		NSDate EnrollmentDate { get; }

		// @property (readonly, copy, nonatomic) NSString * _Nonnull factorID;
		[Export ("factorID")]
		string FactorID { get; }

		// @property (readonly, nonatomic, class) BOOL supportsSecureCoding;
		[Static]
		[Export ("supportsSecureCoding")]
		bool SupportsSecureCoding { get; }

		// -(void)encodeWithCoder:(NSCoder * _Nonnull)coder;
		[Export ("encodeWithCoder:")]
		void EncodeWithCoder (NSCoder coder);

		// -(instancetype _Nullable)initWithCoder:(NSCoder * _Nonnull)coder __attribute__((objc_designated_initializer));
		[Export ("initWithCoder:")]
		[DesignatedInitializer]
		NativeHandle Constructor (NSCoder coder);
	}
	// @interface FIRMultiFactorResolver : NSObject
	[TV (13, 0), MacCatalyst (13, 0), Mac (10, 15), iOS (13, 0)]
	[BaseType (typeof (NSObject))]
	[DisableDefaultCtor]
	interface FIRMultiFactorResolver {
		// @property (readonly, nonatomic, strong) FIRMultiFactorSession * _Nonnull session;
		[Export ("session", ArgumentSemantic.Strong)]
		FIRMultiFactorSession Session { get; }

		// @property (readonly, copy, nonatomic) NSArray<FIRMultiFactorInfo *> * _Nonnull hints;
		[Export ("hints", ArgumentSemantic.Copy)]
		FIRMultiFactorInfo [] Hints { get; }

		// @property (readonly, nonatomic, strong) FIRAuth * _Nonnull auth;
		[Export ("auth", ArgumentSemantic.Strong)]
		FIRAuth Auth { get; }

		// -(void)resolveSignInWithAssertion:(FIRMultiFactorAssertion * _Nonnull)assertion completion:(void (^ _Nullable)(FIRAuthDataResult * _Nullable, NSError * _Nullable))completion;
		[Export ("resolveSignInWithAssertion:completion:")]
		void ResolveSignInWithAssertion (FIRMultiFactorAssertion assertion, [NullAllowed] Action<FIRAuthDataResult, NSError> completion);
	}

	// @interface FIRMultiFactorSession : NSObject
	[TV (13, 0), MacCatalyst (13, 0), Mac (10, 15), iOS (13, 0)]
	[BaseType (typeof (NSObject))]
	[DisableDefaultCtor]
	interface FIRMultiFactorSession {
	}
// // 
// // 	// @interface FIROAuthCredential : FIRAuthCredential <NSSecureCoding>
// // 	[TV (13, 0), MacCatalyst (13, 0), Mac (10, 15), iOS (13, 0)]
// // 	[BaseType (typeof (FIRAuthCredential))]
// // 	interface FIROAuthCredential : INSSecureCoding {
// // 		// @property (readonly, copy, nonatomic) NSString * _Nullable IDToken;
// // 		[NullAllowed, Export ("IDToken")]
// // 		string IDToken { get; }
// // 
// // 		// @property (readonly, copy, nonatomic) NSString * _Nullable accessToken;
// // 		[NullAllowed, Export ("accessToken")]
// // 		string AccessToken { get; }
// // 
// // 		// @property (readonly, copy, nonatomic) NSString * _Nullable secret;
// // 		[NullAllowed, Export ("secret")]
// // 		string Secret { get; }
// // 
// // 		// @property (readonly, nonatomic, class) BOOL supportsSecureCoding;
// // 		[Static]
// // 		[Export ("supportsSecureCoding")]
// // 		bool SupportsSecureCoding { get; }
// // 
// // 		// -(void)encodeWithCoder:(NSCoder * _Nonnull)coder;
// // 		[Export ("encodeWithCoder:")]
// // 		void EncodeWithCoder (NSCoder coder);
// // 
// // 		// -(instancetype _Nullable)initWithCoder:(NSCoder * _Nonnull)coder __attribute__((objc_designated_initializer));
// // 		[Export ("initWithCoder:")]
// // 		[DesignatedInitializer]
// // 		NativeHandle Constructor (NSCoder coder);
// // 	}
// 
// 	// @interface FIROAuthProvider : NSObject <FIRFederatedAuthProvider>
// 	[TV (13, 0), MacCatalyst (13, 0), Mac (10, 15), iOS (13, 0)]
// 	[BaseType (typeof (NSObject))]
// 	[DisableDefaultCtor]
// 	interface FIROAuthProvider {
// 		// @property (readonly, copy, nonatomic, class) NSString * _Nonnull id;
// 		[Static]
// 		[Export ("id")]
// 		string Id { get; }
// 
// 		// @property (copy, nonatomic) NSArray<NSString *> * _Nullable scopes;
// 		[NullAllowed, Export ("scopes", ArgumentSemantic.Copy)]
// 		string [] Scopes { get; set; }
// 
// 		// @property (copy, nonatomic) NSDictionary<NSString *,NSString *> * _Nullable customParameters;
// 		[NullAllowed, Export ("customParameters", ArgumentSemantic.Copy)]
// 		NSDictionary<NSString, NSString> CustomParameters { get; set; }
// 
// 		// @property (readonly, copy, nonatomic) NSString * _Nonnull providerID;
// 		[Export ("providerID")]
// 		string ProviderID { get; }
// 
// 		// +(FIROAuthProvider * _Nonnull)providerWithProviderID:(NSString * _Nonnull)providerID __attribute__((warn_unused_result("")));
// 		[Static]
// 		[Export ("providerWithProviderID:")]
// 		FIROAuthProvider ProviderWithProviderID (string providerID);
// 
// 		// +(FIROAuthProvider * _Nonnull)providerWithProviderID:(NSString * _Nonnull)providerID auth:(FIRAuth * _Nonnull)auth __attribute__((warn_unused_result("")));
// 		[Static]
// 		[Export ("providerWithProviderID:auth:")]
// 		FIROAuthProvider ProviderWithProviderID (string providerID, FIRAuth auth);
// 
// 		// +(FIROAuthCredential * _Nonnull)credentialWithProviderID:(NSString * _Nonnull)providerID IDToken:(NSString * _Nonnull)idToken accessToken:(NSString * _Nullable)accessToken __attribute__((warn_unused_result("")));
// 		[Static]
// 		[Export ("credentialWithProviderID:IDToken:accessToken:")]
// 		FIROAuthCredential CredentialWithProviderID (string providerID, string idToken, [NullAllowed] string accessToken);
// 
// 		// +(FIROAuthCredential * _Nonnull)credentialWithProviderID:(NSString * _Nonnull)providerID accessToken:(NSString * _Nonnull)accessToken __attribute__((warn_unused_result("")));
// 		[Static]
// 		[Export ("credentialWithProviderID:accessToken:")]
// 		FIROAuthCredential CredentialWithProviderID (string providerID, string accessToken);
// 
// 		// +(FIROAuthCredential * _Nonnull)credentialWithProviderID:(NSString * _Nonnull)providerID IDToken:(NSString * _Nonnull)idToken rawNonce:(NSString * _Nonnull)rawNonce accessToken:(NSString * _Nonnull)accessToken __attribute__((warn_unused_result("")));
// 		[Static]
// 		[Export ("credentialWithProviderID:IDToken:rawNonce:accessToken:")]
// 		FIROAuthCredential CredentialWithProviderID (string providerID, string idToken, string rawNonce, string accessToken);
// 
// 		// +(FIROAuthCredential * _Nonnull)credentialWithProviderID:(NSString * _Nonnull)providerID IDToken:(NSString * _Nonnull)idToken rawNonce:(NSString * _Nonnull)rawNonce __attribute__((warn_unused_result("")));
// 		[Static]
// 		[Export ("credentialWithProviderID:IDToken:rawNonce:")]
// 		FIROAuthCredential CredentialWithProviderID (string providerID, string idToken, string rawNonce);
// 
// 		// +(FIROAuthCredential * _Nonnull)appleCredentialWithIDToken:(NSString * _Nonnull)idToken rawNonce:(NSString * _Nullable)rawNonce fullName:(NSPersonNameComponents * _Nullable)fullName __attribute__((warn_unused_result("")));
// 		[Static]
// 		[Export ("appleCredentialWithIDToken:rawNonce:fullName:")]
// 		FIROAuthCredential AppleCredentialWithIDToken (string idToken, [NullAllowed] string rawNonce, [NullAllowed] NSPersonNameComponents fullName);
// 	}
// 
// 	// @interface FIRPhoneAuthCredential : FIRAuthCredential <NSSecureCoding>
// 	[TV (13, 0), MacCatalyst (13, 0), Mac (10, 15), iOS (13, 0)]
// 	[BaseType (typeof (FIRAuthCredential))]
// 	interface FIRPhoneAuthCredential : INSSecureCoding {
// 		// @property (readonly, nonatomic, class) BOOL supportsSecureCoding;
// 		[Static]
// 		[Export ("supportsSecureCoding")]
// 		bool SupportsSecureCoding { get; }
// 
// 		// -(void)encodeWithCoder:(NSCoder * _Nonnull)coder;
// 		[Export ("encodeWithCoder:")]
// 		void EncodeWithCoder (NSCoder coder);
// 
// 		// -(instancetype _Nullable)initWithCoder:(NSCoder * _Nonnull)coder __attribute__((objc_designated_initializer));
// 		[Export ("initWithCoder:")]
// 		[DesignatedInitializer]
// 		NativeHandle Constructor (NSCoder coder);
// 	}

	// @interface FIRPhoneAuthProvider : NSObject
	[TV (13, 0), MacCatalyst (13, 0), Mac (10, 15), iOS (13, 0)]
	[BaseType (typeof (NSObject))]
	interface FIRPhoneAuthProvider {
		// @property (readonly, copy, nonatomic, class) NSString * _Nonnull id;
		[Static]
		[Export ("id")]
		string Id { get; }
	}

	// @interface FIRPhoneMultiFactorAssertion : FIRMultiFactorAssertion
	[TV (13, 0), MacCatalyst (13, 0), Mac (10, 15), iOS (13, 0)]
	[BaseType (typeof (FIRMultiFactorAssertion))]
	interface FIRPhoneMultiFactorAssertion {
	}

	// @interface FIRPhoneMultiFactorGenerator : NSObject
	[TV (13, 0), MacCatalyst (13, 0), Mac (10, 15), iOS (13, 0)]
	[BaseType (typeof (NSObject))]
	interface FIRPhoneMultiFactorGenerator {// 
// 		// +(FIRPhoneMultiFactorAssertion * _Nonnull)assertionWithCredential:(FIRPhoneAuthCredential * _Nonnull)phoneAuthCredential __attribute__((warn_unused_result("")));
// 		[Static]
// 		[Export ("assertionWithCredential:")]
// 		FIRPhoneMultiFactorAssertion AssertionWithCredential (FIRPhoneAuthCredential phoneAuthCredential);
// 
	}

	// @interface FIRPhoneMultiFactorInfo : FIRMultiFactorInfo
	[BaseType (typeof (FIRMultiFactorInfo))]
	interface FIRPhoneMultiFactorInfo {
		// @property (readonly, copy, nonatomic, class) NSString * _Nonnull FIRPhoneMultiFactorID;
		[Static]
		[Export ("FIRPhoneMultiFactorID")]
		string FIRPhoneMultiFactorID { get; }

		// @property (readonly, copy, nonatomic, class) NSString * _Nonnull FIRTOTPMultiFactorID;
		[Static]
		[Export ("FIRTOTPMultiFactorID")]
		string FIRTOTPMultiFactorID { get; }

		// @property (copy, nonatomic) NSString * _Nonnull phoneNumber;
		[Export ("phoneNumber")]
		string PhoneNumber { get; set; }

		// @property (readonly, nonatomic, class) BOOL supportsSecureCoding;
		[Static]
		[Export ("supportsSecureCoding")]
		bool SupportsSecureCoding { get; }

		// -(instancetype _Nullable)initWithCoder:(NSCoder * _Nonnull)coder __attribute__((objc_designated_initializer));
		[Export ("initWithCoder:")]
		[DesignatedInitializer]
		NativeHandle Constructor (NSCoder coder);

		// -(void)encodeWithCoder:(NSCoder * _Nonnull)coder;
		[Export ("encodeWithCoder:")]
		void EncodeWithCoder (NSCoder coder);
	}

	// @interface FIRTOTPMultiFactorAssertion : FIRMultiFactorAssertion
	[BaseType (typeof (FIRMultiFactorAssertion))]
	interface FIRTOTPMultiFactorAssertion {
	}

	// @interface FIRTOTPMultiFactorGenerator : NSObject
	[TV (13, 0), MacCatalyst (13, 0), Mac (10, 15), iOS (13, 0)]
	[BaseType (typeof (NSObject))]
	interface FIRTOTPMultiFactorGenerator {
		// +(void)generateSecretWithMultiFactorSession:(FIRMultiFactorSession * _Nonnull)session completion:(void (^ _Nonnull)(FIRTOTPSecret * _Nullable, NSError * _Nullable))completion;
		[Static]
		[Export ("generateSecretWithMultiFactorSession:completion:")]
		void GenerateSecretWithMultiFactorSession (FIRMultiFactorSession session, Action<FIRTOTPSecret, NSError> completion);

		// +(FIRTOTPMultiFactorAssertion * _Nonnull)assertionForEnrollmentWithSecret:(FIRTOTPSecret * _Nonnull)secret oneTimePassword:(NSString * _Nonnull)oneTimePassword __attribute__((warn_unused_result("")));
		[Static]
		[Export ("assertionForEnrollmentWithSecret:oneTimePassword:")]
		FIRTOTPMultiFactorAssertion AssertionForEnrollmentWithSecret (FIRTOTPSecret secret, string oneTimePassword);

		// +(FIRTOTPMultiFactorAssertion * _Nonnull)assertionForSignInWithEnrollmentID:(NSString * _Nonnull)enrollmentID oneTimePassword:(NSString * _Nonnull)oneTimePassword __attribute__((warn_unused_result("")));
		[Static]
		[Export ("assertionForSignInWithEnrollmentID:oneTimePassword:")]
		FIRTOTPMultiFactorAssertion AssertionForSignInWithEnrollmentID (string enrollmentID, string oneTimePassword);
	}

	// @interface FIRTOTPSecret : NSObject
	[BaseType (typeof (NSObject))]
	[DisableDefaultCtor]
	interface FIRTOTPSecret {
		// -(NSString * _Nonnull)sharedSecretKey __attribute__((warn_unused_result("")));
		[Export ("sharedSecretKey")]
		string SharedSecretKey { get; }

		// -(NSString * _Nonnull)generateQRCodeURLWithAccountName:(NSString * _Nonnull)accountName issuer:(NSString * _Nonnull)issuer __attribute__((warn_unused_result("")));
		[Export ("generateQRCodeURLWithAccountName:issuer:")]
		string GenerateQRCodeURLWithAccountName (string accountName, string issuer);

		// -(void)openInOTPAppWithQRCodeURL:(NSString * _Nonnull)qrCodeURL;
		[Export ("openInOTPAppWithQRCodeURL:")]
		void OpenInOTPAppWithQRCodeURL (string qrCodeURL);
	}

	// @interface FIRTwitterAuthProvider : NSObject
	[TV (13, 0), MacCatalyst (13, 0), Mac (10, 15), iOS (13, 0)]
	[BaseType (typeof (NSObject))]
	[DisableDefaultCtor]
	interface FIRTwitterAuthProvider {
		// @property (readonly, copy, nonatomic, class) NSString * _Nonnull id;
		[Static]
		[Export ("id")]
		string Id { get; }

		// +(FIRAuthCredential * _Nonnull)credentialWithToken:(NSString * _Nonnull)token secret:(NSString * _Nonnull)secret __attribute__((warn_unused_result("")));
		[Static]
		[Export ("credentialWithToken:secret:")]
		FIRAuthCredential CredentialWithToken (string token, string secret);
	}

	// @protocol FIRUserInfo <NSObject>
	/*
  Check whether adding [Model] to this declaration is appropriate.
  [Model] is used to generate a C# class that implements this protocol,
  and might be useful for protocols that consumers are supposed to implement,
  since consumers can subclass the generated class instead of implementing
  the generated interface. If consumers are not supposed to implement this
  protocol, then [Model] is redundant and will generate code that will never
  be used.
*/
	[Protocol]
	[BaseType (typeof (NSObject))]
	interface FIRUserInfo {
		// @required @property (readonly, copy, nonatomic) NSString * _Nonnull providerID;
		[Abstract]
		[Export ("providerID")]
		string ProviderID { get; }

		// @required @property (readonly, copy, nonatomic) NSString * _Nonnull uid;
		[Abstract]
		[Export ("uid")]
		string Uid { get; }

		// @required @property (readonly, copy, nonatomic) NSString * _Nullable displayName;
		[Abstract]
		[NullAllowed, Export ("displayName")]
		string DisplayName { get; }

		// @required @property (readonly, copy, nonatomic) NSURL * _Nullable photoURL;
		[Abstract]
		[NullAllowed, Export ("photoURL", ArgumentSemantic.Copy)]
		NSUrl PhotoURL { get; }

		// @required @property (readonly, copy, nonatomic) NSString * _Nullable email;
		[Abstract]
		[NullAllowed, Export ("email")]
		string Email { get; }

		// @required @property (readonly, copy, nonatomic) NSString * _Nullable phoneNumber;
		[Abstract]
		[NullAllowed, Export ("phoneNumber")]
		string PhoneNumber { get; }
	}

	// @interface FIRUser : NSObject <FIRUserInfo>
	[TV (13, 0), MacCatalyst (13, 0), Mac (10, 15), iOS (13, 0)]
	[BaseType (typeof (NSObject))]
	[DisableDefaultCtor]
	interface FIRUser {
		// @property (readonly, nonatomic) BOOL isAnonymous;
		[Export ("isAnonymous")]
		bool IsAnonymous { get; }

		// -(BOOL)anonymous __attribute__((warn_unused_result("")));
		[Export ("anonymous")]
		bool Anonymous { get; }

		// @property (readonly, nonatomic) BOOL isEmailVerified;
		[Export ("isEmailVerified")]
		bool IsEmailVerified { get; }

		// -(BOOL)emailVerified __attribute__((warn_unused_result("")));
		[Export ("emailVerified")]
		bool EmailVerified { get; }

		// @property (readonly, copy, nonatomic) NSArray<id<FIRUserInfo>> * _Nonnull providerData;
		[Export ("providerData", ArgumentSemantic.Copy)]
		FIRUserInfo [] ProviderData { get; }

		// @property (readonly, nonatomic, strong) FIRUserMetadata * _Nonnull metadata;
		[Export ("metadata", ArgumentSemantic.Strong)]
		FIRUserMetadata Metadata { get; }

		// @property (readonly, copy, nonatomic) NSString * _Nullable tenantID;
		[NullAllowed, Export ("tenantID")]
		string TenantID { get; }

		// @property (readonly, nonatomic, strong) FIRMultiFactor * _Nonnull multiFactor;
		[Export ("multiFactor", ArgumentSemantic.Strong)]
		FIRMultiFactor MultiFactor { get; }

		// -(void)updateEmail:(NSString * _Nonnull)email completion:(void (^ _Nullable)(NSError * _Nullable))completion __attribute__((deprecated("`updateEmail` is deprecated and will be removed in a future release. Use sendEmailVerification(beforeUpdatingEmail:) instead.")));
		[Export ("updateEmail:completion:")]
		void UpdateEmail (string email, [NullAllowed] Action<NSError> completion);

		// -(void)updatePassword:(NSString * _Nonnull)password completion:(void (^ _Nullable)(NSError * _Nullable))completion;
		[Export ("updatePassword:completion:")]
		void UpdatePassword (string password, [NullAllowed] Action<NSError> completion);

		// -(FIRUserProfileChangeRequest * _Nonnull)profileChangeRequest __attribute__((warn_unused_result("")));
		[Export ("profileChangeRequest")]
		FIRUserProfileChangeRequest ProfileChangeRequest { get; }

		// @property (readonly, copy, nonatomic) NSString * _Nullable refreshToken;
		[NullAllowed, Export ("refreshToken")]
		string RefreshToken { get; }

		// -(void)reloadWithCompletion:(void (^ _Nullable)(NSError * _Nullable))completion;
		[Export ("reloadWithCompletion:")]
		void ReloadWithCompletion ([NullAllowed] Action<NSError> completion);

		// -(void)reauthenticateWithCredential:(FIRAuthCredential * _Nonnull)credential completion:(void (^ _Nullable)(FIRAuthDataResult * _Nullable, NSError * _Nullable))completion;
		[Export ("reauthenticateWithCredential:completion:")]
		void ReauthenticateWithCredential (FIRAuthCredential credential, [NullAllowed] Action<FIRAuthDataResult, NSError> completion);

		// -(void)getIDTokenWithCompletion:(void (^ _Nullable)(NSString * _Nullable, NSError * _Nullable))completion;
		[Export ("getIDTokenWithCompletion:")]
		void GetIDTokenWithCompletion ([NullAllowed] Action<NSString, NSError> completion);

		// -(void)getIDTokenForcingRefresh:(BOOL)forceRefresh completion:(void (^ _Nullable)(NSString * _Nullable, NSError * _Nullable))completion;
		[Export ("getIDTokenForcingRefresh:completion:")]
		void GetIDTokenForcingRefresh (bool forceRefresh, [NullAllowed] Action<NSString, NSError> completion);

		// -(void)getIDTokenResultWithCompletion:(void (^ _Nullable)(FIRAuthTokenResult * _Nullable, NSError * _Nullable))completion;
		[Export ("getIDTokenResultWithCompletion:")]
		void GetIDTokenResultWithCompletion ([NullAllowed] Action<FIRAuthTokenResult, NSError> completion);

		// -(void)getIDTokenResultForcingRefresh:(BOOL)forcingRefresh completion:(void (^ _Nullable)(FIRAuthTokenResult * _Nullable, NSError * _Nullable))completion;
		[Export ("getIDTokenResultForcingRefresh:completion:")]
		void GetIDTokenResultForcingRefresh (bool forcingRefresh, [NullAllowed] Action<FIRAuthTokenResult, NSError> completion);

		// -(void)linkWithCredential:(FIRAuthCredential * _Nonnull)credential completion:(void (^ _Nullable)(FIRAuthDataResult * _Nullable, NSError * _Nullable))completion;
		[Export ("linkWithCredential:completion:")]
		void LinkWithCredential (FIRAuthCredential credential, [NullAllowed] Action<FIRAuthDataResult, NSError> completion);

		// -(void)unlinkFromProvider:(NSString * _Nonnull)provider completion:(void (^ _Nullable)(FIRUser * _Nullable, NSError * _Nullable))completion;
		[Export ("unlinkFromProvider:completion:")]
		void UnlinkFromProvider (string provider, [NullAllowed] Action<FIRUser, NSError> completion);

		// -(void)sendEmailVerificationWithCompletion:(void (^ _Nullable)(NSError * _Nullable))completion;
		[Export ("sendEmailVerificationWithCompletion:")]
		void SendEmailVerificationWithCompletion ([NullAllowed] Action<NSError> completion);

		// -(void)sendEmailVerificationWithActionCodeSettings:(FIRActionCodeSettings * _Nullable)actionCodeSettings completion:(void (^ _Nullable)(NSError * _Nullable))completion;
		[Export ("sendEmailVerificationWithActionCodeSettings:completion:")]
		void SendEmailVerificationWithActionCodeSettings ([NullAllowed] FIRActionCodeSettings actionCodeSettings, [NullAllowed] Action<NSError> completion);

		// -(void)deleteWithCompletion:(void (^ _Nullable)(NSError * _Nullable))completion;
		[Export ("deleteWithCompletion:")]
		void DeleteWithCompletion ([NullAllowed] Action<NSError> completion);

		// -(void)sendEmailVerificationBeforeUpdatingEmail:(NSString * _Nonnull)email completion:(void (^ _Nullable)(NSError * _Nullable))completion;
		[Export ("sendEmailVerificationBeforeUpdatingEmail:completion:")]
		void SendEmailVerificationBeforeUpdatingEmail (string email, [NullAllowed] Action<NSError> completion);

		// -(void)sendEmailVerificationBeforeUpdatingEmail:(NSString * _Nonnull)email actionCodeSettings:(FIRActionCodeSettings * _Nullable)actionCodeSettings completion:(void (^ _Nullable)(NSError * _Nullable))completion;
		[Export ("sendEmailVerificationBeforeUpdatingEmail:actionCodeSettings:completion:")]
		void SendEmailVerificationBeforeUpdatingEmail (string email, [NullAllowed] FIRActionCodeSettings actionCodeSettings, [NullAllowed] Action<NSError> completion);

		// @property (readonly, copy, nonatomic) NSString * _Nonnull providerID;
		[Export ("providerID")]
		string ProviderID { get; }

		// @property (readonly, copy, nonatomic) NSString * _Nonnull uid;
		[Export ("uid")]
		string Uid { get; }

		// @property (copy, nonatomic) NSString * _Nullable displayName;
		[NullAllowed, Export ("displayName")]
		string DisplayName { get; set; }

		// @property (copy, nonatomic) NSURL * _Nullable photoURL;
		[NullAllowed, Export ("photoURL", ArgumentSemantic.Copy)]
		NSUrl PhotoURL { get; set; }

		// @property (readonly, copy, nonatomic) NSString * _Nullable email;
		[NullAllowed, Export ("email")]
		string Email { get; }

		// @property (readonly, copy, nonatomic) NSString * _Nullable phoneNumber;
		[NullAllowed, Export ("phoneNumber")]
		string PhoneNumber { get; }

		// @property (readonly, nonatomic, class) BOOL supportsSecureCoding;
		[Static]
		[Export ("supportsSecureCoding")]
		bool SupportsSecureCoding { get; }

		// -(void)encodeWithCoder:(NSCoder * _Nonnull)coder;
		[Export ("encodeWithCoder:")]
		void EncodeWithCoder (NSCoder coder);

		// -(instancetype _Nullable)initWithCoder:(NSCoder * _Nonnull)coder __attribute__((objc_designated_initializer));
		[Export ("initWithCoder:")]
		[DesignatedInitializer]
		NativeHandle Constructor (NSCoder coder);
	}
	// @interface FIRUserMetadata : NSObject
	[TV (13, 0), MacCatalyst (13, 0), Mac (10, 15), iOS (13, 0)]
	[BaseType (typeof (NSObject))]
	[DisableDefaultCtor]
	interface FIRUserMetadata {
		// @property (readonly, copy, nonatomic) NSDate * _Nullable lastSignInDate;
		[NullAllowed, Export ("lastSignInDate", ArgumentSemantic.Copy)]
		NSDate LastSignInDate { get; }

		// @property (readonly, copy, nonatomic) NSDate * _Nullable creationDate;
		[NullAllowed, Export ("creationDate", ArgumentSemantic.Copy)]
		NSDate CreationDate { get; }

		// @property (readonly, nonatomic, class) BOOL supportsSecureCoding;
		[Static]
		[Export ("supportsSecureCoding")]
		bool SupportsSecureCoding { get; }

		// -(void)encodeWithCoder:(NSCoder * _Nonnull)coder;
		[Export ("encodeWithCoder:")]
		void EncodeWithCoder (NSCoder coder);

		// -(instancetype _Nullable)initWithCoder:(NSCoder * _Nonnull)coder;
		[Export ("initWithCoder:")]
		NativeHandle Constructor (NSCoder coder);
	}
	// @interface FIRUserProfileChangeRequest : NSObject
	[TV (13, 0), MacCatalyst (13, 0), Mac (10, 15), iOS (13, 0)]
	[BaseType (typeof (NSObject))]
	[DisableDefaultCtor]
	interface FIRUserProfileChangeRequest {
		// @property (copy, nonatomic) NSString * _Nullable displayName;
		[NullAllowed, Export ("displayName")]
		string DisplayName { get; set; }

		// @property (copy, nonatomic) NSURL * _Nullable photoURL;
		[NullAllowed, Export ("photoURL", ArgumentSemantic.Copy)]
		NSUrl PhotoURL { get; set; }

		// -(void)commitChangesWithCompletion:(void (^ _Nullable)(NSError * _Nullable))completion;
		[Export ("commitChangesWithCompletion:")]
		void CommitChangesWithCompletion ([NullAllowed] Action<NSError> completion);
	}
}
