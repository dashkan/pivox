# Downloads Firebase C++ SDK pre-built desktop libraries at configure time.
# Usage: DownloadFirebaseCpp("13.5.0" "${CMAKE_SOURCE_DIR}/third_party/firebase-cpp")

function(DownloadFirebaseCpp version download_dir)
    set(FIREBASE_DIR "${download_dir}/firebase_cpp_sdk")

    if(EXISTS "${FIREBASE_DIR}")
        message(STATUS "Firebase C++ SDK ${version} already downloaded")
        set(FIREBASE_CPP_ROOT "${FIREBASE_DIR}" PARENT_SCOPE)
        return()
    endif()

    set(FIREBASE_URL "https://dl.google.com/firebase/sdk/cpp/firebase_cpp_sdk_${version}.zip")
    set(FIREBASE_ZIP "${download_dir}/firebase_cpp_sdk_${version}.zip")

    message(STATUS "Downloading Firebase C++ SDK ${version} from ${FIREBASE_URL}...")
    file(MAKE_DIRECTORY "${download_dir}")

    file(DOWNLOAD "${FIREBASE_URL}" "${FIREBASE_ZIP}"
        SHOW_PROGRESS
        STATUS download_status)
    list(GET download_status 0 status_code)
    if(NOT status_code EQUAL 0)
        message(FATAL_ERROR "Failed to download Firebase C++ SDK: ${download_status}")
    endif()

    message(STATUS "Extracting Firebase C++ SDK...")
    file(ARCHIVE_EXTRACT INPUT "${FIREBASE_ZIP}" DESTINATION "${download_dir}")
    file(REMOVE "${FIREBASE_ZIP}")

    set(FIREBASE_CPP_ROOT "${FIREBASE_DIR}" PARENT_SCOPE)
endfunction()
