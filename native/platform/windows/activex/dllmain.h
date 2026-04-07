#pragma once

class CPivoxActiveXModule : public ATL::CAtlDllModuleT<CPivoxActiveXModule>
{
public:
    DECLARE_LIBID(LIBID_PivoxActiveXLib)
    DECLARE_REGISTRY_APPID_RESOURCEID(IDR_PIVOXACTIVEX,
        "{D5C4B3A2-F6E5-4B7A-9D8C-1F0E2A3B4C5D}")
};

extern class CPivoxActiveXModule _AtlModule;
