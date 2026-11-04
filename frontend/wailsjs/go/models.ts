export namespace album {
	
	export class Record {
	    id: string;
	    type: string;
	    imagePath?: string;
	    packetPath?: string;
	    imageWidth?: number;
	    imageHeight?: number;
	    // Go type: time
	    createdAt: any;
	    profile: string;
	    callsign?: string;
	    imageId?: number;
	    sessionId?: string;
	    packetsReceived?: number;
	    packetsExpected?: number;
	    quality?: string;
	    status: string;
	
	    static createFrom(source: any = {}) {
	        return new Record(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.type = source["type"];
	        this.imagePath = source["imagePath"];
	        this.packetPath = source["packetPath"];
	        this.imageWidth = source["imageWidth"];
	        this.imageHeight = source["imageHeight"];
	        this.createdAt = this.convertValues(source["createdAt"], null);
	        this.profile = source["profile"];
	        this.callsign = source["callsign"];
	        this.imageId = source["imageId"];
	        this.sessionId = source["sessionId"];
	        this.packetsReceived = source["packetsReceived"];
	        this.packetsExpected = source["packetsExpected"];
	        this.quality = source["quality"];
	        this.status = source["status"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace audio {
	
	export class DeviceInfo {
	    id: string;
	    name: string;
	    isDefault: boolean;
	
	    static createFrom(source: any = {}) {
	        return new DeviceInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.isDefault = source["isDefault"];
	    }
	}

}

export namespace domain {
	
	export class Profile {
	    id: string;
	    name: string;
	    imageCodec: string;
	    linkLayer: string;
	    bitRate: number;
	    sampleRate: number;
	    modulation: string;
	    markFrequency: number;
	    spaceFrequency: number;
	
	    static createFrom(source: any = {}) {
	        return new Profile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.imageCodec = source["imageCodec"];
	        this.linkLayer = source["linkLayer"];
	        this.bitRate = source["bitRate"];
	        this.sampleRate = source["sampleRate"];
	        this.modulation = source["modulation"];
	        this.markFrequency = source["markFrequency"];
	        this.spaceFrequency = source["spaceFrequency"];
	    }
	}

}

export namespace gallery {
	
	export class Asset {
	    id: string;
	    name: string;
	    path: string;
	    // Go type: time
	    addedAt: any;
	    width?: number;
	    height?: number;
	
	    static createFrom(source: any = {}) {
	        return new Asset(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.path = source["path"];
	        this.addedAt = this.convertValues(source["addedAt"], null);
	        this.width = source["width"];
	        this.height = source["height"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace main {
	
	export class AudioDeviceLists {
	    inputs: audio.DeviceInfo[];
	    outputs: audio.DeviceInfo[];
	
	    static createFrom(source: any = {}) {
	        return new AudioDeviceLists(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.inputs = this.convertValues(source["inputs"], audio.DeviceInfo);
	        this.outputs = this.convertValues(source["outputs"], audio.DeviceInfo);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class AudioState {
	    connected: boolean;
	    rxActive: boolean;
	    txActive: boolean;
	    sampleRate: number;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new AudioState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.connected = source["connected"];
	        this.rxActive = source["rxActive"];
	        this.txActive = source["txActive"];
	        this.sampleRate = source["sampleRate"];
	        this.error = source["error"];
	    }
	}
	export class RenderedTXImage {
	    id: string;
	    name: string;
	    pngBase64: string;
	
	    static createFrom(source: any = {}) {
	        return new RenderedTXImage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.pngBase64 = source["pngBase64"];
	    }
	}
	export class WaterfallSettings {
	    minFrequencyHz: number;
	    maxFrequencyHz: number;
	    minDbfs: number;
	    maxDbfs: number;
	    rowsPerSecond: number;
	    historySeconds: number;
	    backgroundColor: string;
	    lowColor: string;
	    midColor: string;
	    highColor: string;
	    peakColor: string;
	
	    static createFrom(source: any = {}) {
	        return new WaterfallSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.minFrequencyHz = source["minFrequencyHz"];
	        this.maxFrequencyHz = source["maxFrequencyHz"];
	        this.minDbfs = source["minDbfs"];
	        this.maxDbfs = source["maxDbfs"];
	        this.rowsPerSecond = source["rowsPerSecond"];
	        this.historySeconds = source["historySeconds"];
	        this.backgroundColor = source["backgroundColor"];
	        this.lowColor = source["lowColor"];
	        this.midColor = source["midColor"];
	        this.highColor = source["highColor"];
	        this.peakColor = source["peakColor"];
	    }
	}
	export class Settings {
	    language: string;
	    rxProfile: string;
	    txProfile: string;
	    quality: string;
	    callsign: string;
	    ax25Ssid: number;
	    locator: string;
	    audioSampleRate: number;
	    rxInputDevice: string;
	    txOutputDevice: string;
	    templateBackgroundAssetId?: string;
	    waterfall: WaterfallSettings;
	    ptt: ptt.Config;
	    pttLeadMs: number;
	    pttTailMs: number;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.language = source["language"];
	        this.rxProfile = source["rxProfile"];
	        this.txProfile = source["txProfile"];
	        this.quality = source["quality"];
	        this.callsign = source["callsign"];
	        this.ax25Ssid = source["ax25Ssid"];
	        this.locator = source["locator"];
	        this.audioSampleRate = source["audioSampleRate"];
	        this.rxInputDevice = source["rxInputDevice"];
	        this.txOutputDevice = source["txOutputDevice"];
	        this.templateBackgroundAssetId = source["templateBackgroundAssetId"];
	        this.waterfall = this.convertValues(source["waterfall"], WaterfallSettings);
	        this.ptt = this.convertValues(source["ptt"], ptt.Config);
	        this.pttLeadMs = source["pttLeadMs"];
	        this.pttTailMs = source["pttTailMs"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace ptt {
	
	export class Config {
	    mode: string;
	    rigctlAddress: string;
	    serialPort: string;
	    activeHigh: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Config(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mode = source["mode"];
	        this.rigctlAddress = source["rigctlAddress"];
	        this.serialPort = source["serialPort"];
	        this.activeHigh = source["activeHigh"];
	    }
	}
	export class TestState {
	    active: boolean;
	    // Go type: time
	    startedAt?: any;
	    // Go type: time
	    endsAt?: any;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new TestState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.active = source["active"];
	        this.startedAt = this.convertValues(source["startedAt"], null);
	        this.endsAt = this.convertValues(source["endsAt"], null);
	        this.error = source["error"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace template {
	
	export class Component {
	    id: string;
	    type: string;
	    text?: string;
	    assetId?: string;
	    x: number;
	    y: number;
	    width?: number;
	    height?: number;
	    scale?: number;
	    fontSize?: number;
	    bold?: boolean;
	    color?: string;
	    visible: boolean;
	    outline: boolean;
	    outlineWidth: number;
	    outlineColor?: string;
	
	    static createFrom(source: any = {}) {
	        return new Component(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.type = source["type"];
	        this.text = source["text"];
	        this.assetId = source["assetId"];
	        this.x = source["x"];
	        this.y = source["y"];
	        this.width = source["width"];
	        this.height = source["height"];
	        this.scale = source["scale"];
	        this.fontSize = source["fontSize"];
	        this.bold = source["bold"];
	        this.color = source["color"];
	        this.visible = source["visible"];
	        this.outline = source["outline"];
	        this.outlineWidth = source["outlineWidth"];
	        this.outlineColor = source["outlineColor"];
	    }
	}
	export class Template {
	    id: string;
	    name: string;
	    backgroundAssetId?: string;
	    components: Component[];
	    updatedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new Template(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.backgroundAssetId = source["backgroundAssetId"];
	        this.components = this.convertValues(source["components"], Component);
	        this.updatedAt = source["updatedAt"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

