# Downloads Firebase Apple SDK xcframeworks at configure time.
# Usage: DownloadFirebase("11.12.0" "${CMAKE_SOURCE_DIR}/third_party/firebase")

function(DownloadFirebase version download_dir)
    set(FIREBASE_ZIP "${download_dir}/Firebase-${version}.zip")
    set(FIREBASE_DIR "${download_dir}/Firebase")

    if(EXISTS "${FIREBASE_DIR}")
        message(STATUS "Firebase ${version} already downloaded")
        set(FIREBASE_ROOT "${FIREBASE_DIR}" PARENT_SCOPE)
        return()
    endif()

    set(FIREBASE_URL "https://github.com/firebase/firebase-ios-sdk/releases/download/${version}/Firebase.zip")
    message(STATUS "Downloading Firebase ${version} from ${FIREBASE_URL}...")

    file(DOWNLOAD "${FIREBASE_URL}" "${FIREBASE_ZIP}"
        SHOW_PROGRESS
        STATUS download_status)
    list(GET download_status 0 status_code)
    if(NOT status_code EQUAL 0)
        message(FATAL_ERROR "Failed to download Firebase: ${download_status}")
    endif()

    message(STATUS "Extracting Firebase...")
    file(ARCHIVE_EXTRACT INPUT "${FIREBASE_ZIP}" DESTINATION "${download_dir}")
    file(REMOVE "${FIREBASE_ZIP}")

    set(FIREBASE_ROOT "${FIREBASE_DIR}" PARENT_SCOPE)
endfunction()
