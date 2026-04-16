import Foundation

public enum ChatError: LocalizedError {
    case initFailed
    case streamFailed(String)
    case serverError(String)
    case unknownTool(String)

    public var errorDescription: String? {
        switch self {
        case .initFailed:
            return "Failed to initialize chat client"
        case .streamFailed(let msg):
            return "Stream failed: \(msg)"
        case .serverError(let msg):
            return "Server error: \(msg)"
        case .unknownTool(let name):
            return "Unknown tool: \(name)"
        }
    }
}
