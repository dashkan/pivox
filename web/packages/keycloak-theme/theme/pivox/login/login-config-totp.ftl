<#import "template.ftl" as layout>
<#import "password-commons.ftl" as passwordCommons>
<#import "commons.ftl" as commons>
<@layout.registrationLayout displayRequiredFields=false displayMessage=!messagesPerField.existsError('totp','userLabel'); section>

    <#if section = "header">
        ${msg("loginTotpTitle")}
    <#elseif section = "form">
        <div id="kc-totp-settings" class="flex flex-col gap-4 px-4 text-sm text-muted-foreground">
            <div>
                <p>${msg("loginTotpStep1")}</p>
                <ul id="kc-totp-supported-apps" class="mt-1 list-disc ps-5">
                    <#list totp.supportedApplications as app>
                        <li>${msg(app)}</li>
                    </#list>
                </ul>
            </div>

            <#if mode?? && mode = "manual">
                <div>
                    <p>${msg("loginTotpManualStep2")}</p>
                    <p id="kc-totp-secret-key" class="mt-1 rounded-md border border-border bg-muted px-3 py-2 font-mono text-foreground break-all">${totp.totpSecretEncoded}</p>
                    <p class="mt-1"><a href="${totp.qrUrl}" id="mode-barcode" class="kc-link">${msg("loginTotpScanBarcode")}</a></p>
                </div>
                <div>
                    <p>${msg("loginTotpManualStep3")}</p>
                    <ul class="mt-1 list-disc ps-5">
                        <li id="kc-totp-type">${msg("loginTotpType")}: ${msg("loginTotp." + totp.policy.type)}</li>
                        <li id="kc-totp-algorithm">${msg("loginTotpAlgorithm")}: ${totp.policy.getAlgorithmKey()}</li>
                        <li id="kc-totp-digits">${msg("loginTotpDigits")}: ${totp.policy.digits}</li>
                        <#if totp.policy.type = "totp">
                            <li id="kc-totp-period">${msg("loginTotpInterval")}: ${totp.policy.period}</li>
                        <#elseif totp.policy.type = "hotp">
                            <li id="kc-totp-counter">${msg("loginTotpCounter")}: ${totp.policy.initialCounter}</li>
                        </#if>
                    </ul>
                </div>
            <#else>
                <div>
                    <p>${msg("loginTotpStep2")}</p>
                    <div class="mt-2 flex justify-center">
                        <img id="kc-totp-secret-qr-code" class="size-48 rounded-lg bg-white p-3" src="data:image/png;base64, ${totp.totpSecretQrCode}" alt="Figure: Barcode" />
                    </div>
                    <p class="mt-2 text-center"><a href="${totp.manualUrl}" id="mode-manual" class="kc-link">${msg("loginTotpUnableToScan")}</a></p>
                </div>
            </#if>

            <div>
                <p>${msg("loginTotpStep3")}</p>
                <p>${msg("loginTotpStep3DeviceName")}</p>
            </div>
        </div>

        <form action="${url.loginAction}" class="flex flex-col gap-4" id="kc-totp-settings-form" method="post">
            <div class="kc-field">
                <label for="totp" class="kc-label">${msg("authenticatorCode")} <span class="required">*</span></label>
                <@commons.otpInput id="totp" name="totp" length=totp.policy.digits
                    ariaInvalid=messagesPerField.existsError('totp') />

                <#if messagesPerField.existsError('totp')>
                    <span id="input-error-otp-code" class="kc-field-error" aria-live="polite">
                        ${kcSanitize(messagesPerField.get('totp'))?no_esc}
                    </span>
                </#if>

                <input type="hidden" id="totpSecret" name="totpSecret" value="${totp.totpSecret}" />
                <#if mode??><input type="hidden" id="mode" name="mode" value="${mode}"/></#if>
            </div>

            <div class="kc-field">
                <label for="userLabel" class="kc-label">${msg("loginTotpDeviceName")} <#if totp.otpCredentials?size gte 1><span class="required">*</span></#if></label>
                <input type="text" class="kc-input" id="userLabel" name="userLabel" autocomplete="off"
                       aria-invalid="<#if messagesPerField.existsError('userLabel')>true</#if>" dir="ltr"
                />

                <#if messagesPerField.existsError('userLabel')>
                    <span id="input-error-otp-label" class="kc-field-error" aria-live="polite">
                        ${kcSanitize(messagesPerField.get('userLabel'))?no_esc}
                    </span>
                </#if>
            </div>

            <@passwordCommons.logoutOtherSessions/>

            <div id="kc-form-buttons" class="kc-actions">
                <#if isAppInitiatedAction??>
                    <input type="submit"
                           class="kc-btn kc-btn-primary"
                           id="saveTOTPBtn" value="${msg("doSubmit")}"
                    />
                    <button type="submit"
                            class="kc-btn kc-btn-outline"
                            id="cancelTOTPBtn" name="cancel-aia" value="true">${msg("doCancel")}</button>
                <#else>
                    <input type="submit"
                           class="kc-btn kc-btn-primary"
                           id="saveTOTPBtn" value="${msg("doSubmit")}"
                    />
                </#if>
            </div>
        </form>
        <@commons.otpScript />
    </#if>
</@layout.registrationLayout>
