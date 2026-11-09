# ADR 0007: Sign-in with a code from e-mail, and four roles

Status: accepted, 2026-11-10.

## Context

Relatives need a simple and safe way into the tree. They are often not technical and use a phone. A password is one more thing to choose, to forget and to reuse. A tree editor and an administrator must be able to invite family members.

Gramps Web uses a name and a password, or OpenID Connect, and has no invitations.

## Decision

- **No passwords.** An account is an e-mail address. To sign in, a person types the address and then a code of 8 digits that Heirfold sends to it. Heirfold has no password hash to steal and needs no dependency for password hashing.
- **Four roles.** Guest (0), family member (1), tree editor (3), administrator (5). The numbers match the Gramps Web roles of the same name. Heirfold has no role for the Gramps Web numbers 2 (Contributor) and 4 (Owner).

  | Role | Sees | Can do |
  | --- | --- | --- |
  | Guest | People who have died, no private records | Read |
  | Family member | The whole tree, without the hidden notes | Read, download the tree, choose the own portrait. With a setting: change the own branch |
  | Tree editor | The whole tree | Change the tree, invite guests and family members |
  | Administrator | The whole tree | All of that, and give any role, remove accounts, import, export, settings |

- **An invitation is an account.** The inviter gives an address and a role. Heirfold creates the account and sends a letter with the address of the site. The invited person then signs in like everybody else. No invitation token exists, so none can leak.
- **An account can name its person in the tree.** Most people with access are in the tree themselves. The inviter can choose the person, an administrator can change the choice, and each user can set the own link. The tree then opens on that person. The link gives no rights, so it needs no fresh code. A user can name only a person that the user can see. Heirfold keeps the handle of the person, so the link survives new IDs in Gramps, and it shows nothing when the person leaves the tree.
- **A family member can get the right to change the own branch.** It is a setting next to the role, not a role: it depends on the link to a person, and the numbers of the roles stay the same as in Gramps Web. The branch is the person of the account, the descendants, and the partners of all of them. Only an administrator gives the right, a short time after a sign-in code. While the right is on, the user cannot change the own link to a person, because that link now decides what the user can change. [ADR 0008](0008-editing.md) says how the server checks each edit.
- **A letter uses the language of the account.** The language of the page serves only an account that has no language yet. A browser in another language, or a stranger who asks for a code, does not change what the owner reads. The inviter chooses the language of an invitation. After a sign-in the page takes the language of the account.
- **A hidden note is for editors and administrators only.** A note with the private flag holds what the keepers of the tree do not tell every relative: an unchecked family story, a guess of a researcher. A family member and a guest get no word of it: the page of a person leaves it out, the edit form of the own branch leaves it out and answers "not found" for it, and the file of the tree for a family member holds such a note with an empty text. Every other private record keeps its rule: hidden from guests, open to family members. An editor sets the mark in the form of a note; a new text without a word about the mark keeps it.
- **A family member can download the whole tree.** A family member sees the whole tree, so the Gramps XML file gives nothing new. A guest gets no file.
- **A session lasts one year from its last use.** The server moves the end of a session forward at most once a day and renews the cookie at the same time. A person sees the signed-in devices and can end them.
- **Sensitive actions need a fresh code.** A change of a role or of an address, the removal of a person who has signed in, and an invitation of an editor or an administrator work only for 30 minutes after a sign-in code. After that the page asks for a new code. A long session on a shared computer therefore cannot change who controls the tree.
- **Three ways to send a letter.** SMTP for any mail server or mailbox; the HTTP API of Resend for hosts that block the SMTP ports; and the server log, for a server on the computer of its administrator. A letter is plain text.

## How a code is protected

| Risk | Protection |
| --- | --- |
| A guess of the code | 8 digits, 5 tries for each code, 10 minutes of life, at most 5 live codes for an account, rate limits for each client and for each address. A constant attack on one account succeeds with a chance of about 0.4% in a year. |
| A code typed into another browser | The browser that asks for a code gets a secret cookie. The code works only with that cookie. |
| A stolen database | The database keeps a hash of the code together with the cookie secret, and only a hash of each session token. |
| A question "does this address have an account?" | The answer is the same for a known and an unknown address. The work for a known address runs after the answer. |
| A flood of letters | Limits for each client, for each address and for the whole server. Letters to known browsers, letters to new browsers and invitations have separate allowances. |
| A stranger uses up the codes of a relative | A browser that signed in before carries a device cookie, a keyed hash of the address. It has its own allowance. |
| A header injected into a letter | Addresses are checked, a header with a line break is refused, and the body is base64. |
| An address in a log | Logs name an account by its number. The audit log names it by its number and its display name. An invitation needs a display name, so no name comes from an address. |

## Consequences

- The mailbox is the key. A person who reads the mail of a relative can sign in as that relative. This is the same as a password reset by e-mail, which most services have.
- A server on the internet needs a way to send letters. Without one, each code goes to the server log, and the start-up output says so.
- An attacker who knows an address and has many client addresses can use up the code allowance of that account for a time. The owner then waits some minutes.
- Passkeys, two-factor sign-in and OpenID Connect stay on the roadmap. They can come on top of this design.
- If a person loses the mailbox, the operator gives the account a new address with `heirfold user-email`. That command ends all sessions of the account.
