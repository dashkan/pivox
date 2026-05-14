using Grpc.Core;
using Pivox.Shared.Auth;

namespace Pivox.Client.Auth;

/// <summary>
/// gRPC <see cref="CallCredentials"/> that attaches
/// <c>Authorization: Bearer &lt;jwt&gt;</c> to every outgoing RPC's
/// metadata, async-correctly. Reads the token from
/// <see cref="IAuthService.GetIdTokenAsync"/>; the underlying Firebase
/// SDK refreshes the token internally when it's close to expiry.
///
/// Why CallCredentials and not an Interceptor: gRPC client
/// interceptors are synchronous at the metadata-attachment seam —
/// any token fetch would have to .Wait/.Result, which deadlocks
/// the main thread when the token fetch needs to await a callback
/// dispatched back to that same thread (e.g., Firebase's
/// getIDTokenWithCompletion: on macOS). CallCredentials' interceptor
/// delegate is async-native — gRPC awaits the metadata fetch
/// without blocking any thread.
///
/// When the user isn't signed in, no header is attached. The server's
/// auth interceptor will reject with UNAUTHENTICATED, surfacing the
/// requirement at the right layer.
/// </summary>
public static class AuthCallCredentials
{
    public static CallCredentials FromAuthService(IAuthService auth)
    {
        return CallCredentials.FromInterceptor(async (context, metadata) =>
        {
            if (auth.Current is null) return;

            try
            {
                var token = await auth.GetIdTokenAsync();
                metadata.Add("Authorization", $"Bearer {token}");
            }
            catch (Exception ex)
            {
                // Let the call proceed without auth so the server can
                // reject with UNAUTHENTICATED. Don't swallow silently —
                // log so unexpected fetch failures are visible.
                Console.Error.WriteLine(
                    $"[Auth] token fetch failed for outbound gRPC: {ex.GetType().Name}: {ex.Message}");
            }
        });
    }
}
