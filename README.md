# rss-butt-plug

> :peach: **e x p e r i m e n t a l** :peach:

A [SSB](https://scuttlebutt.nz) client which "plugs" a RSS feed into the Scuttleverse.

Created in the same spirit as [Lykin](https://git.coopcloud.tech/glyph/lykin),
for educational purposes and to help others dip a toe into the SSB development
ecosystem. It's mostly a demo and I don't necessarily intend to maintain it.

Here's a [short
screencast](http://vvvvvvaria.org/~decentral1se/ssb/rss-butt-plug/rbpc.mp4)
with a quick tour of the tool and some thoughts on it.

**Please** read the [Be Gentle](#be-gentle-monocle_face) section before using it on the mainnet.

## Scuttlin' :nerd_face:

Get a local copy of `rss-butt-plug`:

```
curl https://vvvvvvaria.org/~decentral1se/ssb/rss-butt-plug/rss-butt-plug_linux_amd64_v1/rss-butt-plug -o rss-butt-plug
chmod +x rss-butt-plug
```

If you're not running `amd64` arch, take a look in [vvvvvvaria.org/~decentral1se/ssb/rss-butt-plug](https://vvvvvvaria.org/~decentral1se/ssb/rss-butt-plug) for binaries which suit your system.

Create a `rss-butt-plug.yaml` in the same directory:

```yaml
---
# where all data will be stored, is a relative path to the current working directory
data-dir: .rss-butt-plug

# the RSS feed URL
feed: https://openrss.org/opencollective.com/secure-scuttlebutt-consortium/updates

# the RSS feed profile avatar URL (will be converted to blob)
avatar: https://images.opencollective.com/secure-scuttlebutt-consortium/676f245/logo/256.png

# the internal go-sbot configuration options
addr: localhost
port: 8008
ws-port: 8989
shs-cap: "1KHLiKZvAvjbY1ziZEHMXawbCEIM6qwjCDm3VYRan/s="
```

Run it:

```
./rss-butt-plug
```

Wire up a local Patchwork client (or something that supports *legacy*
replication, [see FAQ for
more](https://github.com/ssbc/go-ssb/blob/master/docs/faq.md#what-is-legacy-replication))
for reading & sharing with the broader SSB ecosystem.

`rss-butt-plug` generates an invite every time it runs which you can use to
invite clients with. Feeds will be polled every 5 minutes by default, you can
configure this with `-p`.

## Limitations :stop_sign:

* Multiple RSS feeds are not supported. It would be great to have but I don't
  know how to do it (afaiu, `go-sbot` is one identity per-instance). You could
  run multiple instances of `rss-butt-plug`, an instance per RSS feed. You'd
  need to tweak the ports in the `rss-butt-plug.yaml` to not have conflicts but
  it could work.

* The HTML -> Markdown might be a bit dodgy, so  I would recommend doing some
  testing on local throwaway Patchwork / `rss-butt-plug` identities before
  doing any mainnet replication. You can test parse a feed by passing it as an
  argument, e.g. `./rss-butt-plug https://laipower.xyz/rss` and the first post
  will be shown.

* All `go-ssb` experimental caveats apply, see [the
  FAQ](https://github.com/ssbc/go-ssb/blob/master/docs/faq.md) for more.

## Be gentle :monocle_face:

You may feel the sudden urge to plug loads of RSS feeds into the Scuttleverse.
I certainly did. However, with some testing, I quickly realised this isn't a
very good idea.

One example, a lot of great SSB people are also on the Fediverse but the RSS
feeds provided by Mastodon trim the titles and don't carry over the images in
the posts properly (have tested in a couple of feed readers).

Also, you can't `@` a plug identity on SSB because there are no humans behind
it. So, if you plug an RSS feed connected with other aspects of your online
life then how do people speak to you?

I think it's a question of going slow and waiting for people to test out the
tool and see what dynamic it produces. Please test your feeds first, see how it
looks, see what content the feed provides and ofc, see if it "fits" being
plugged into SSB.

## Internal features :computer:

Some internal things that might be nice to know / refer to if also hacking on
this or building your own stuff. I would guess that a lot of the following
ingredients could be generalised into other kinds of clients, scripts and
tools.

`rss-butt-plug` does the following...

* Internally manages a `go-sbot` instance, handy for emdedding magic scuttlin'
  superpowers in your Go scripts.

* Creates a TCP client which makes requests to the managed `go-sbot` via the
  MUXRPC interface. E.g. generates an invite.

* In general, uses some "direct" Go API bindings which work on the local log.
  These APIs are more low-level and involved than the MUXRPC interface but are
  more powerful.

* Image uploads. The HTML is parsed to look for images while converting it to
  Markdown. When an image is found, it is uploaded as a blob and then the blob
  ref replaces the traditional link. Clients like Patchwork then know how to
  show the images in the renderer.

* Breaking up large posts into root + reply threads so that we do not go over
  the length limit of a post. The implementation of this is quite a hack, so go
  easy on me.

* Uses a `goreleaser` config to create cross-platform binaries.

## Inspo :sunflower:

* `%fzitwDLKcKgGL3RKv6BrBHzQRr1qC7QCeC6PpirMwwE=.sha256`
* `%JmVycMs1KqxH0LYzXDnbhNCKH9sCOtBZE/yDU+cBWoY=.sha256`

## ACK :+1:

* [`go-ssb`](https://github.com/ssbc/go-ssb)
* [`egonelbre/gophers`](https://github.com/egonelbre/gophers)

## LICENSE

<a href="https://git.coopcloud.tech/decentral1se/rss-butt-plug/src/branch/main/LICENSE">
  <img src="https://www.gnu.org/graphics/gplv3-or-later.png" />
</a>
