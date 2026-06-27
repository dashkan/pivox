<#import "template.ftl" as layout>
<@layout.registrationLayout; section>
    <#if section = "header">
        ${msg("frontchannel-logout.title")}
    <#elseif section = "form">
        <p class="kc-prose">${msg("frontchannel-logout.message")}</p>
        <ul class="kc-prose">
        <#list logout.clients as client>
            <li>
                ${client.name}
                <iframe src="${client.frontChannelLogoutUrl}" style="display:none;"></iframe>
            </li>
        </#list>
        </ul>
        <#if logout.logoutRedirectUri?has_content>
            <script>
                <#outputformat "JavaScript">
                function readystatechange(event) {
                    if (document.readyState=='complete') {
                        window.location.replace(${logout.logoutRedirectUri?c});
                    }
                }
                document.addEventListener('readystatechange', readystatechange);
                </#outputformat>
            </script>
            <div class="kc-actions">
                <a id="continue" class="kc-btn kc-btn-primary" href="${logout.logoutRedirectUri}">${msg("doContinue")}</a>
            </div>
        </#if>
    </#if>
</@layout.registrationLayout>
