#pragma once

// {E5F6A7B8-C9D0-4E1F-8A2B-3C4D5E6F7A8B}
struct __declspec(uuid("E5F6A7B8-C9D0-4E1F-8A2B-3C4D5E6F7A8B")) _IPivoxControlEvents;

using namespace ATL;

template <class T>
class CProxy_IPivoxControlEvents :
    public IConnectionPointImpl<T, &__uuidof(_IPivoxControlEvents), CComDynamicUnkArray>
{
public:
};
