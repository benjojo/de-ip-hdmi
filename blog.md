Ludicrously cheap HDMI capture for Linux
===

[![the inside of the HDMI sender](https://blog.benjojo.co.uk/asset/GLj5iyIgvy)](https://blog.benjojo.co.uk/asset/HV1vqrFBOb)

Lately I have had the need to do real time video capture from HDMI devices as of late for a project, and while looking around the internet found that all of the capture cards that are aimed at gamers (windows / OSX support only) or full blown production capture (Very expensive, more inputs than I need). The other downside is that all of these options either have no Linux drivers at all, or if they do, they have a Linux driver that is behind an NDA, and those cards are in the $800+ range.

Looking around more, I wondered if you could hack up something that _just so happened_ to do HDMI capture and hijack off that, It happened that my prayers were heard and danman made a blog post about hacking up IP based HDMI extenders to dump the output. [Link](https://blog.danman.eu/reverse-engineering-lenkeng-hdmi-over-ip-extender/) ( [Archive 1](http://archive.is/ThNO1) | [Archive 2](https://web.archive.org/web/20160626203245/https://blog.danman.eu/reverse-engineering-lenkeng-hdmi-over-ip-extender/) )

After reading though that, I fired up eBay and picked up an [identical looking model](http://archive.is/DUQfS) fairly cheaply for £42.

A few days later, the two units arrived, both with a 5V DC barrel plug (I ended up powering them from hacked up USB cables during my testing rather than the wall adapters) and as far as I could tell, worked fine, not that I ever plugged in the receiver into any screen to check the output :)

Using the scripts that danman made, I confirmed that things were mostly the same (and that their scripts worked out of the box for video), and I started to hack away at making a better program to capture the stream from the devices. Interestingly this whole process was a lot easier thanks to the cheapness of my desk switch, because it was unable to do [IGMP snooping](https://en.wikipedia.org/wiki/IGMP_snooping), and thus spat out every multi cast packet that the sender made into every other port on the switch (back the good old days of hubs).

I noticed that my version of the device was one higher than danman had on his post ( I have version 2 written on my PCB ), I was expecting the worst in terms of changes, and yet very little had changed. 

I found that the audio sample rate had changed to 44100hz, down from the 48000hz that danman reported. (Though this could be dependent on the input?)

One of the more nasty issues I had to deal with was the activation sequence, since the sender will only send when it knows that there is a receiver ready to listen for it and I had to do more digging on the activation system. I bought wireshark out and started to plug the receiver in and out while watching the traffic to see if I could spot any clues. After a few plugs I finally gained a clue:

![an wild arp packet appears](https://blog.benjojo.co.uk/asset/8Bnq0Va0fx)

Every second, the sender will send out a packet to multicast with a packet that lets the multicast group know it exists, and how long it's been up. When the receiver sees one of these packets, it asks through ARP for the IP address of that (hardcoded) IP. I gathered the reason it needed to ask for this is that it sends a packet to it directly to the MAC address of the sender to activate it.

I recorded a bunch of these packets and singled them out in wireshark. Then swapped the MAC addresses using [tcprewrite](http://tcpreplay.synfin.net/wiki/tcprewrite)

`tcprewrite --enet-smac=90:2b:34:31:02:0b --infile=activator.pcap --outfile=replay.pcap`

then I replayed that pcap file with tcpreplay:

![terminal showing use of tcpreplay](https://blog.benjojo.co.uk/asset/WKqrv8L0WW)

and saw the receiver biting on the bait! After reconfiguring my interfaces, I finally had the magic packet!

After realising that with this version of the unit you could not send these packets to broadcast to trigger activation, I addressed its MAC address directly and we had full transmission without the receiver being plugged in at all!

I've improved upon the original set of scripts that dump these devices, you can find them [here on github](https://github.com/benjojo/de-ip-hdmi) and they should now be more stable and nicer to use, It also uses less CPU to output:

![only about 25% of a core to do this](https://blog.benjojo.co.uk/asset/EeEm5eFbDn)

Giving I was planning to use more than one of these devices on the same network and system, my version of the tool uses libpcap and binds to a interface, the idea being is that you attach of these devices directly to it's own NIC for isolation, and this program will be able to dump more than one HDMI stream at the same time.

Other improvements have been made, For example by default it will output [matroska, aka mkv](https://www.matroska.org/) with both video and audio so that it can be easily interfaced with (no compression happens in this stage, just containerisation).

You can pipe the tool directly into ffmpeg or vlc and then work with the content from there!

![Only a small bitrate](https://blog.benjojo.co.uk/asset/cdAOZkOiiu)

The code for this project can be found on my github: [github.com/benjojo/de-ip-hdmi](https://github.com/benjojo/de-ip-hdmi) or there is a prebuild deb file that is ready to go (Ubuntu and Debian should be fine) here: [Link](https://github.com/benjojo/de-ip-hdmi/releases/tag/0.1)

### Quality samples

These units are not perfect for quality, however they are not bad at all, Here is a screenshot taken on my laptop, vs what is captured and sent:

Before:
[![before thumbnail](https://blog.benjojo.co.uk/asset/0UidBfYfRK)](https://blog.benjojo.co.uk/asset/3E0aFhkuEK)

After:
[![after thumbnail](https://blog.benjojo.co.uk/asset/NQDuAHOAD8)](https://blog.benjojo.co.uk/asset/5MT8Y8mnlO)
