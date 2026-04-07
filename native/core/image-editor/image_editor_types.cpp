#include "image_editor_types.h"

namespace pivox {

bool EditState::operator==(const EditState& other) const {
    return cropWidth == other.cropWidth &&
           cropHeight == other.cropHeight &&
           rotation == other.rotation &&
           straighten == other.straighten &&
           scale == other.scale &&
           tx == other.tx &&
           ty == other.ty &&
           flipHorizontal == other.flipHorizontal &&
           flipVertical == other.flipVertical &&
           resizeMode == other.resizeMode;
    // Note: activeTemplate comparison is by label (templates are value types)
}

} // namespace pivox
