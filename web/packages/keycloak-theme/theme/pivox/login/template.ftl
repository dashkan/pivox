<#--
  Pivox login theme shell. Rewrites the base `registrationLayout` macro to
  render the Pivox auth card (centered, max-w-sm, ring + rounded-xl) instead
  of the PatternFly layout, while preserving every Keycloak hook: the macro
  signature, the nested "header"/"form"/"socialProviders"/"info" sections,
  the SSO session-polling + authChecker scripts, and password-visibility JS.

  Styling comes from the .kc-* component layer (resources/css/theme.css,
  compiled from src/shared.css). Per-page templates that aren't overridden
  fall back to the kc*Class property mappings in theme.properties.

  Dark mode honors Keycloak's native realm-level "Dark mode" toggle: when the
  gated `darkMode` variable is true (theme darkMode=true AND realm toggle on),
  the script below toggles `.dark` on <html> from the OS color scheme; when
  off, the page stays light.
-->
<#import "footer.ftl" as loginFooter>
<#import "commons.ftl" as commons>
<#macro registrationLayout bodyClass="" displayInfo=false displayMessage=true displayRequiredFields=false>
<!DOCTYPE html>
<html class="${properties.kcHtmlClass!}" lang="${lang}"<#if realm.internationalizationEnabled> dir="${(locale.rtl)?then('rtl','ltr')}"</#if>>

<head>
    <meta charset="utf-8">
    <meta http-equiv="Content-Type" content="text/html; charset=UTF-8" />
    <meta name="viewport" content="width=device-width,initial-scale=1" />
    <meta name="color-scheme" content="light${darkMode?then(' dark', '')}" />
    <title>${msg("loginTitle",(realm.displayName!''))}</title>
    <link rel="icon" href="${url.resourcesPath}/img/favicon.ico" />

    <#-- Dark mode gated by the realm's native "Dark mode" toggle. Only emitted
         when `darkMode` is true; toggles `.dark` from the OS scheme with no
         flash (blocking="render"). -->
    <#if darkMode>
        <script type="module" async blocking="render">
            <#outputformat "JavaScript">
            const DARK_MODE_CLASS = ${properties.kcDarkModeClass?c};
            const mediaQuery = window.matchMedia("(prefers-color-scheme: dark)");
            updateDarkMode(mediaQuery.matches);
            mediaQuery.addEventListener("change", (event) => updateDarkMode(event.matches));
            function updateDarkMode(isEnabled) {
                document.documentElement.classList.toggle(DARK_MODE_CLASS, isEnabled);
            }
            </#outputformat>
        </script>
    </#if>

    <#if properties.styles?has_content>
        <#list properties.styles?split(' ') as style>
            <link href="${url.resourcesPath}/${style}" rel="stylesheet" />
        </#list>
    </#if>
    <#if properties.scripts?has_content>
        <#list properties.scripts?split(' ') as script>
            <script src="${url.resourcesPath}/${script}" type="text/javascript"></script>
        </#list>
    </#if>
    <script type="importmap">
        {
            "imports": {
                "rfc4648": "${url.resourcesCommonPath}/vendor/rfc4648/rfc4648.js"
            }
        }
    </script>
    <script src="${url.resourcesPath}/js/menu-button-links.js" type="module"></script>
    <#if scripts??>
        <#list scripts as script>
            <script src="${script}" type="text/javascript"></script>
        </#list>
    </#if>
    <script type="module">
        <#outputformat "JavaScript">
        import { startSessionPolling } from ${(url.resourcesPath + "/js/authChecker.js")?c};

        startSessionPolling(
            ${url.ssoLoginInOtherTabsUrl?c}
        );
        </#outputformat>
    </script>
    <script type="module">
        document.addEventListener("click", (event) => {
            const link = event.target.closest("a[data-once-link]");
            if (!link) {
                return;
            }
            if (link.getAttribute("aria-disabled") === "true") {
                event.preventDefault();
                return;
            }
            const { disabledClass } = link.dataset;
            if (disabledClass) {
                link.classList.add(...disabledClass.trim().split(/\s+/));
            }
            link.setAttribute("role", "link");
            link.setAttribute("aria-disabled", "true");
        });
    </script>
    <#if authenticationSession??>
        <script type="module">
            <#outputformat "JavaScript">
            import { checkAuthSession } from ${(url.resourcesPath + "/js/authChecker.js")?c};

            checkAuthSession(
                ${authenticationSession.authSessionIdHash?c}
            );
            </#outputformat>
        </script>
    </#if>
</head>

<body class="${properties.kcBodyClass!}" data-page-id="login-${pageId}">
<div class="kc-page ${bodyClass}">
    <div class="kc-card">
        <div class="kc-stack">

            <#-- Locale switcher (only when i18n enabled with >1 language). -->
            <#if realm.internationalizationEnabled && locale.supported?size gt 1>
                <div class="kc-locale" id="kc-locale">
                    <div id="kc-locale-wrapper" class="menu-button-links relative">
                        <button id="kc-current-locale-link" class="kc-locale-current"
                                aria-label="${msg("languages")}" aria-haspopup="true" aria-expanded="false"
                                aria-controls="language-switch1">${locale.current}</button>
                        <ul role="menu" tabindex="-1" aria-labelledby="kc-current-locale-link"
                            aria-activedescendant="" id="language-switch1" class="kc-locale-menu" hidden>
                            <#list locale.supported as l>
                                <li role="none">
                                    <a role="menuitem" class="kc-locale-item" href="${l.url}">${l.label}</a>
                                </li>
                            </#list>
                        </ul>
                    </div>
                </div>
            </#if>

            <#-- Page title (the "header" nested section). -->
            <div class="kc-header">
                <#if !(auth?has_content && auth.showUsername() && !auth.showResetCredentials())>
                    <h1 id="kc-page-title" class="kc-title"><#nested "header"></h1>
                <#else>
                    <#nested "show-username">
                    <h1 id="kc-page-title" class="kc-title"><#nested "header"></h1>
                    <#-- Reauth identity: the known user as a readonly input
                         (tabbable, full width — matches the password field) with
                         a reset button on the right. The reset hits
                         loginRestartFlowUrl (skip_logout=false), which clears the
                         IdP session and starts a fresh login — so the user can
                         switch accounts, not just re-auth as this one. -->
                    <#-- Field layout WITHOUT kc-field's px-4: the surrounding
                         .kc-header already applies px-4, so the input-group lines
                         up exactly with the password field below it. -->
                    <div id="kc-username" class="flex w-full flex-col gap-2 text-left">
                        <label for="kc-attempted-username" class="kc-label"><#if !realm.loginWithEmailAllowed>${msg("username")}<#elseif !realm.registrationEmailAsUsername>${msg("usernameOrEmail")}<#else>${msg("email")}</#if></label>
                        <div class="kc-input-group" dir="ltr">
                            <input id="kc-attempted-username" class="kc-input" type="text" value="${auth.attemptedUsername}" readonly dir="ltr" />
                            <a id="reset-login" class="kc-input-toggle" href="${url.loginRestartFlowUrl}"
                               title="${msg('restartLoginTooltip')}" aria-label="${msg('restartLoginTooltip')}">
                                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
                                    <path d="M3 12a9 9 0 0 1 9-9 9.75 9.75 0 0 1 6.74 2.74L21 8"/>
                                    <path d="M21 3v5h-5"/>
                                    <path d="M21 12a9 9 0 0 1-9 9 9.75 9.75 0 0 1-6.74-2.74L3 16"/>
                                    <path d="M3 21v-5h5"/>
                                </svg>
                            </a>
                        </div>
                    </div>
                </#if>
            </div>

            <#-- Global feedback message (success/warning/error/info). -->
            <#if displayMessage && message?has_content && (message.type != 'warning' || !isAppInitiatedAction??)>
                <@commons.alert type=(message.type='error')?then('error', message.type) role=(message.type='error')?then('alert','status')>${kcSanitize(message.summary)?no_esc}</@commons.alert>
            </#if>

            <#-- Page body. -->
            <#nested "form">

            <#if auth?has_content && auth.showTryAnotherWayLink()>
                <form id="kc-select-try-another-way-form" action="${url.loginAction}" method="post" class="kc-actions">
                    <input type="hidden" name="tryAnotherWay" value="on"/>
                    <a href="#" id="try-another-way" class="kc-muted-link text-center"
                       onclick="document.forms['kc-select-try-another-way-form'].requestSubmit();return false;">${msg("doTryAnotherWay")}</a>
                </form>
            </#if>

            <#nested "socialProviders">

            <#-- Generic info section (overridden pages render their own footer). -->
            <#if displayInfo>
                <div id="kc-info" class="kc-prose">
                    <#nested "info">
                </div>
            </#if>
        </div>

        <#-- Optional full-bleed footer band (hand-crafted pages supply this). -->
        <#nested "footer">

        <@loginFooter.content/>
    </div>
</div>
</body>
</html>
</#macro>
