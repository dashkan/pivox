# Observable Proto Types — Target Architecture and Current Blockers

## The goal

Every Pivox UI-bindable proto type should be **directly observable** from its
platform's UI framework, with no wrapper layer. A single C++-generated type
per message, shared across all layers:

- Service code, gRPC machinery, shared business logic operate on the
  same concrete type.
- macOS UI binds to it via SwiftUI's observation system.
- Windows UI binds to it via XAML `x:Bind` through WinRT observation.
- Any mutation from any layer — shared C++ service, platform-side
  glue, UI — automatically fires observation notifications.

"No wrappers" isn't aesthetic preference. It's about **binding
consistency**: if observation is a wrapper concern, every consumer has
to route mutations through the wrapper, or observation silently fails.
That split ("observed if you go through the wrapper, ignored if you
touch the proto directly") is the failure mode we want to avoid.

## Current situation

**We do not currently meet this goal on either platform.** We ship
wrapper-based observation on macOS and plan to do the same on Windows
until / unless the blockers below clear.

### macOS: blocked at the language level

Swift's observation system is language-native. There is no C++-side
hook that makes an imported type participate in it.

**`@Observable`** — a Swift macro that rewrites the type definition
to:
1. Conform to the `Observation.Observable` protocol (empty marker).
2. Add an `ObservationRegistrar` **stored property**.
3. Rewrite each observable property into get/set accessors that call
   `registrar.access(self, keyPath: ...)` and
   `registrar.withMutation(of: self, keyPath: ...)`.

The registrar holds per-instance state: registered observers,
thread-safety locks, event-emission closures. It's a Swift class — the
type must allocate and own it at the Swift level.

For a C++-imported type to host an `ObservationRegistrar`:
- Extensions on imported types cannot add stored properties.
- Swift stored properties require Swift-managed storage; the C++
  type's memory layout is C++-native.
- There is no Swift↔C++ interop feature today that lets a C++ type
  declare "I provide an `ObservationRegistrar` in my layout."

Workarounds that look viable on paper but aren't useful in practice:

- **External registrar map** (global `[ObjectIdentifier:
  ObservationRegistrar]`): breaks for C++ structs (no stable identity),
  adds a thread-safe map on every property access, leaks on C++-side
  deallocation. Performance and correctness both poor.
- **Associated objects via Obj-C runtime**: would require the generated
  C++ type to be an Obj-C++ `NSObject` subclass, which means forking
  protobuf-cpp's emission in a different direction and losing most of
  the "one-type" benefit we were after.
- **Combine's older `ObservableObject`**: feasible with Obj-C++ codegen
  (NSObject + KVO + objectWillChange synthesized via bridging). But
  this is the deprecated SwiftUI binding pattern; `@Observable` is the
  forward path. Building on the old pattern buys short-term
  compatibility at the cost of long-term drift.

**Therefore**: on macOS we write a Swift `@Observable` container
(hand-rolled or later codegen'd) that holds proto data. The container
is Swift; the protos inside it are Swift-protobuf structs today. This
is the wrapper-discipline pattern we want to escape but can't yet.

### Windows: viable, deferred pending implementation

WinRT's observation model **is** C++-expressible. The contract:

- Declare a WinRT runtime class in `.idl`.
- Implement `INotifyPropertyChanged` via `winrt::implements<T,
  IInspectable, INotifyPropertyChanged, ...>`.
- Raise `PropertyChanged` in setters; wrap repeated fields in
  `IObservableVector<T>` that raises `VectorChanged` on mutation.
- Emit an activation factory and register the type in the app's
  `.winmd`.

All of this is achievable in C++ generator output. The practical shape:

- Fork `protoc-gen-cpp` (or build a drop-in replacement generator)
  that emits classes inheriting from both
  `google::protobuf::Message` and `winrt::implements<T, ...>`.
- Generate matching `.idl` files alongside the C++.
- Keep libprotobuf for wire serialization — the class is still a
  `google::protobuf::Message` subclass, so `SerializeToString` /
  `ParseFromString` work unchanged.
- Arena allocation is opt-in in protobuf-cpp (not used by default); we
  don't use it client-side, so the COM-ref-counted lifetime model and
  normal heap/stack allocation coexist fine.

**Known costs** that are real but bounded:

- **Fork maintenance**: upstream `protoc-gen-cpp` releases require
  periodic merges. Output shape is reasonably stable.
- **Copy semantics change**: `winrt::implements<>` deletes the copy
  constructor. Code that does `Message m2 = m1;` must switch to
  `Message m2; m2.CopyFrom(m1);`. Protobuf already provides the API.
- **Multi-inheritance ABI**: `google::protobuf::Message`'s vtable and
  `winrt::implements<>`'s COM vtable coexisting. Expected to work on
  modern MSVC and clang-cl; worth validating in a small spike before
  committing to the generator rewrite.
- **IDL + activation factory emission**: new codegen output alongside
  `.pb.h` / `.pb.cc`. Mechanical templating.

**Deferred, not blocked.** We ship wrapper-based WinRT observation
first (new plugin emitting `Foo_Observable` runtime classes that
project proto data). When we've stabilized the chat UI and have
appetite for the fork-maintenance cost, migrate to the direct-emission
model.

## What to watch for on Swift

The Swift side becomes feasible when Apple (or the Swift Evolution
process) ships a mechanism that lets imported types participate in
observation-style protocols requiring per-instance associated storage.
Signals to track:

- **Swift Evolution proposals** tagged Observation, C++ interop, or
  "foreign conformance" — e.g., any future proposal letting extensions
  add stored properties, or letting imported types declare Swift-
  allocated associated storage.
- **Swift-C++ interop roadmap** (`https://www.swift.org/documentation/cxx-interop/status/`)
  — specifically any addition describing "observation support for
  imported types" or protocol conformances with associated Swift-side
  state.
- **WWDC sessions on Observation framework** — Apple sometimes
  telegraphs future capabilities via session roadmaps.
- **Apple Developer Forums and the Swift forums** — community often
  surfaces experimental builds or language proposals before formal
  evolution.

Once a mechanism exists, we revisit:

1. Fork `protoc-gen-cpp` (or replace it) to emit C++ types that declare
   whatever the new mechanism requires (e.g., associated Swift
   storage descriptor, observation conformance metadata).
2. Swift side binds directly to these types. The Swift `@Observable`
   wrapper collapses to a typealias or disappears.
3. Same generator fork that handles Windows WinRT emission handles
   Swift observation emission — both platforms converge on a single
   codegen lineage.

## What we ship today

- **Proto types**: stock `protoc-gen-cpp` → `libprotobuf`-compatible
  C++ classes. swift-protobuf → native Swift structs for macOS.
- **macOS observation**: hand-written Swift `@Observable` container
  types holding swift-protobuf structs. Container mutations drive
  SwiftUI reactivity.
- **Windows observation**: TBD. Target shape is a WinRT observable
  wrapper plugin emitting `*_Observable` runtime classes that project
  stock proto data. Reference: `docs/native/windows-sync-foundations.md`.

## Revisit conditions

Move to the direct-emission model when:

1. Swift gains the interop feature above (hard gate for macOS).
2. The Windows wrapper plugin is in production and we've measured
   enough friction to justify the fork-maintenance cost of a
   unified single-type generator.

Until then: wrappers on both platforms, native observable types on
neither, and service code operates on stock `protoc-gen-cpp` output.
