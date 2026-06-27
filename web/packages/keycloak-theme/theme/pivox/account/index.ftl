<#--
  Boots the Pivox account console SPA (@pivox/account-console). Keycloak's
  AccountConsole endpoint renders this and provides the model variables below;
  the SPA reads them from <script id="environment"> and uses keycloak-js
  (account-console client) + the Account REST API.

  Assets ship in resources/app/ (built by `pnpm -C web/apps/account-console
  build`): index.js (single bundle) + assets/index.css. Referenced via
  ${resourceUrl} so they resolve under Keycloak's hashed resource path.
-->
<!doctype html>
<html lang="${locale!'en'}">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <meta name="color-scheme" content="light${darkMode?then(' dark', '')}" />
    <title>Account · Pivox</title>

    <#-- Dark mode gated by the realm's native "Dark mode" toggle. -->
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

    <link rel="stylesheet" href="${resourceUrl}/app/assets/index.css" />
  </head>
  <body>
    <div id="app"></div>

    <script id="environment" type="application/json">
      {
        "authServerUrl": "${authServerUrl!''}",
        "authUrl": "${authUrl!''}",
        "serverBaseUrl": "${serverBaseUrl!''}",
        "realm": "${realm.name}",
        "clientId": "${clientId!'account-console'}",
        "resourceUrl": "${resourceUrl}",
        "baseUrl": "${baseUrl!''}",
        "referrerName": "${referrerName!''}",
        "referrerUrl": "${referrer_uri!''}",
        "features": {
          "isLinkedAccountsEnabled": ${(isLinkedAccountsEnabled!false)?c},
          "isViewGroupsEnabled": ${(isViewGroupsEnabled!false)?c},
          "isViewOrganizationsEnabled": ${(isViewOrganizationsEnabled!false)?c},
          "deleteAccountAllowed": ${(deleteAccountAllowed!false)?c},
          "updateEmailFeatureEnabled": ${(updateEmailFeatureEnabled!false)?c}
        }
      }
    </script>

    <script type="module" src="${resourceUrl}/app/index.js"></script>
  </body>
</html>
