<#import "template.ftl" as layout>
<#import "commons.ftl" as commons>
<@layout.registrationLayout; section>

    <#if section = "header">
            ${msg("deleteAccountConfirm")}

   <#elseif section = "form">

    <form action="${url.loginAction}" class="flex flex-col gap-4" method="post">

       <@commons.alert type="warning">${msg("irreversibleAction")}</@commons.alert>

       <div class="kc-prose">
           <p>${msg("deletingImplies")}</p>
           <ul>
             <li>${msg("loggingOutImmediately")}</li>
             <li>${msg("errasingData")}</li>
           </ul>
           <p>${msg("finalDeletionConfirmation")}</p>
       </div>

      <div class="kc-actions">
            <input class="kc-btn kc-btn-destructive" type="submit" value="${msg("doConfirmDelete")}" />
            <#if triggered_from_aia>
            <button class="kc-btn kc-btn-outline" type="submit" name="cancel-aia" value="true">${msg("doCancel")}</button>
            </#if>
       </div>
    </form>
   </#if>

</@layout.registrationLayout>
