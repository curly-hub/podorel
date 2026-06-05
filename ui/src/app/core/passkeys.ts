interface CreationOptionsJSON {
  challenge: string;
  user: {
    id: string;
    name?: string;
    displayName?: string;
  };
  excludeCredentials?: CredentialDescriptorJSON[];
  [key: string]: unknown;
}

interface RequestOptionsJSON {
  challenge: string;
  allowCredentials?: CredentialDescriptorJSON[];
  [key: string]: unknown;
}

interface CredentialDescriptorJSON {
  id: string;
  type: PublicKeyCredentialType;
  transports?: AuthenticatorTransport[];
}

export function passkeysSupported(): boolean {
  return typeof window !== 'undefined' && 'PublicKeyCredential' in window && typeof navigator !== 'undefined' && !!navigator.credentials;
}

export function passkeySecureContext(): boolean {
  return typeof window !== 'undefined' && window.isSecureContext === true;
}

export function toPublicKeyCreationOptions(value: unknown): PublicKeyCredentialCreationOptions {
  const options = clone<CreationOptionsJSON>(value);
  return {
    ...options,
    challenge: base64UrlToBuffer(options.challenge),
    user: {
      ...options.user,
      id: base64UrlToBuffer(options.user.id)
    },
    excludeCredentials: options.excludeCredentials?.map((credential) => ({
      ...credential,
      id: base64UrlToBuffer(credential.id)
    }))
  } as PublicKeyCredentialCreationOptions;
}

export function toPublicKeyRequestOptions(value: unknown): PublicKeyCredentialRequestOptions {
  const options = clone<RequestOptionsJSON>(value);
  return {
    ...options,
    challenge: base64UrlToBuffer(options.challenge),
    allowCredentials: options.allowCredentials?.map((credential) => ({
      ...credential,
      id: base64UrlToBuffer(credential.id)
    }))
  } as PublicKeyCredentialRequestOptions;
}

export function credentialToJSON(credential: PublicKeyCredential): Record<string, unknown> {
  const output: Record<string, unknown> = {
    id: credential.id,
    rawId: bufferToBase64Url(credential.rawId),
    type: credential.type,
    response: responseToJSON(credential.response),
    clientExtensionResults: credential.getClientExtensionResults()
  };
  const attachment = (credential as PublicKeyCredential & { authenticatorAttachment?: string }).authenticatorAttachment;
  if (attachment) {
    output['authenticatorAttachment'] = attachment;
  }
  return output;
}

export function passkeyUnavailableMessage(): string {
  if (!passkeySecureContext()) {
    const origin = typeof location !== 'undefined' ? location.origin : 'this origin';
    return `Passkeys need a trusted secure browser context. ${origin} is not trusted by this browser yet. Trust the PoDorel local CA certificate, then reopen PoDorel over HTTPS.`;
  }
  if (!passkeysSupported()) {
    return 'This browser does not support passkeys.';
  }
  return '';
}

export function formatPasskeyError(error: unknown, fallback: string): string {
  if (typeof DOMException !== 'undefined' && error instanceof DOMException) {
    if (error.name === 'NotAllowedError') {
      return 'Passkey prompt was cancelled or timed out.';
    }
    if (error.name === 'SecurityError') {
      return secureContextErrorMessage();
    }
  }
  if (error instanceof Error && looksLikeSecurityError(error.message)) {
    return secureContextErrorMessage();
  }
  return fallback;
}

function secureContextErrorMessage(): string {
  const unavailable = passkeyUnavailableMessage();
  if (unavailable) {
    return unavailable;
  }
  const origin = typeof location !== 'undefined' ? location.origin : 'this origin';
  return `The browser blocked this passkey request as insecure for ${origin}. Make sure the page is opened with HTTPS and the PoDorel local CA is trusted by this browser.`;
}

function looksLikeSecurityError(message: string): boolean {
  return /operation.*(insecure|unsecure|unsecured)|securityerror|insecure context/i.test(message);
}

function responseToJSON(response: AuthenticatorResponse): Record<string, unknown> {
  const output: Record<string, unknown> = {
    clientDataJSON: bufferToBase64Url(response.clientDataJSON)
  };
  if (isAttestationResponse(response)) {
    output['attestationObject'] = bufferToBase64Url(response.attestationObject);
    const transports = response.getTransports?.();
    if (transports?.length) {
      output['transports'] = transports;
    }
  }
  if (isAssertionResponse(response)) {
    output['authenticatorData'] = bufferToBase64Url(response.authenticatorData);
    output['signature'] = bufferToBase64Url(response.signature);
    if (response.userHandle) {
      output['userHandle'] = bufferToBase64Url(response.userHandle);
    }
  }
  return output;
}

function isAttestationResponse(response: AuthenticatorResponse): response is AuthenticatorAttestationResponse {
  return 'attestationObject' in response;
}

function isAssertionResponse(response: AuthenticatorResponse): response is AuthenticatorAssertionResponse {
  return 'authenticatorData' in response;
}

function clone<T>(value: unknown): T {
  return JSON.parse(JSON.stringify(value ?? {})) as T;
}

function base64UrlToBuffer(value: string): ArrayBuffer {
  const normalized = value.replace(/-/g, '+').replace(/_/g, '/');
  const padded = normalized.padEnd(Math.ceil(normalized.length / 4) * 4, '=');
  const binary = atob(padded);
  const bytes = new Uint8Array(binary.length);
  for (let index = 0; index < binary.length; index += 1) {
    bytes[index] = binary.charCodeAt(index);
  }
  return bytes.buffer;
}

function bufferToBase64Url(buffer: ArrayBuffer): string {
  const bytes = new Uint8Array(buffer);
  let binary = '';
  for (let index = 0; index < bytes.byteLength; index += 1) {
    binary += String.fromCharCode(bytes[index]);
  }
  return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/g, '');
}
