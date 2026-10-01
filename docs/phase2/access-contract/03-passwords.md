# Password hashing

This section defines the password hash stored in `accounts.password_hash` from the [account and session contract](01-accounts-and-sessions.md). The [PRD](../../prd.md) requires a suitable password hashing scheme and prohibits plaintext password storage. The Go [`argon2` package](https://pkg.go.dev/golang.org/x/crypto/argon2) provides `IDKey` for Argon2id. The profile below follows [RFC 9106 section 4](https://www.rfc-editor.org/rfc/rfc9106.html#section-4)'s second recommended option for memory-constrained environments, with its recommended 16-byte salt and 32-byte tag lengths.

## Fixed profile

Use Argon2id version 19 with these fixed values:

| Input | Value | Meaning |
| --- | ---: | --- |
| `m` | `65536` KiB | Memory cost, where 1 KiB is 1024 bytes. |
| `t` | `3` | Number of passes over memory. |
| `p` | `4` | Degree of parallelism, also called lanes. |
| Salt | `16` bytes | Fresh cryptographically random bytes for each password hash. |
| Tag | `32` bytes | Derived digest stored with the salt and parameters. |
| Version | `19` (`0x13`) | Argon2 version. |

These are contract values, not runtime benchmark results. Generate a new salt for every password hash using a cryptographically secure random source. If the source fails or returns fewer than 16 bytes, fail the operation and leave the account's stored hash unchanged. Never substitute a fixed, reused, or predictable salt.

Treat the password at the hashing boundary as an opaque byte sequence. Hash the exact bytes supplied by the caller. Do not trim, case-fold, normalize Unicode, truncate, or pre-hash them. Password setup and verification must use the same byte sequence.

## Stored format

Store one Argon2 PHC string with this exact structure and parameter order:

```text
$argon2id$v=19$m=65536,t=3,p=4$<salt>$<digest>
```

`<salt>` is the 16-byte salt encoded with canonical, unpadded standard Base64 and is exactly 22 characters. `<digest>` is the 32-byte tag encoded the same way and is exactly 43 characters. The fields contain no whitespace or padding. The PHC string records the algorithm, version, parameters, salt, and digest in one value; see the [C2SP PHC string specification](https://c2sp.org/phc-strings) for the Argon2 field encoding. RFC 9106 defines the Argon2 version and parameter meanings and recommends 16-byte salts.

The example is a format template. Its angle-bracketed fields are placeholders, not literal encodings or a verified password hash.

## Verification and failures

Before deriving a digest, parse and validate the entire stored value. Accept only the exact algorithm, version, parameter values, field order, canonical unpadded Base64 encoding, and salt and digest lengths defined above. Reject malformed values, noncanonical encodings, and unsupported algorithms, versions, parameters, or fields before hashing. Do not use cost values from an untrusted stored string to configure Argon2.

For an accepted value, derive a 32-byte Argon2id tag from the supplied password bytes and decoded salt using the fixed profile. Compare the candidate tag with the stored digest using a constant-time comparison. A malformed or unsupported stored hash fails verification without changing account data. A salt-generation or hashing failure during a password write aborts that write and leaves the previous hash unchanged.
