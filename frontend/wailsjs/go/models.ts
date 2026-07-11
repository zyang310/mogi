export namespace capture {
	
	export class DisplayInfo {
	    index: number;
	    x: number;
	    y: number;
	    width: number;
	    height: number;
	    label: string;
	
	    static createFrom(source: any = {}) {
	        return new DisplayInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.index = source["index"];
	        this.x = source["x"];
	        this.y = source["y"];
	        this.width = source["width"];
	        this.height = source["height"];
	        this.label = source["label"];
	    }
	}

}

export namespace hotkey {
	
	export class Status {
	    running: boolean;
	    hookEnabled: boolean;
	    spec: string;
	    label: string;
	    goos: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new Status(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.running = source["running"];
	        this.hookEnabled = source["hookEnabled"];
	        this.spec = source["spec"];
	        this.label = source["label"];
	        this.goos = source["goos"];
	        this.error = source["error"];
	    }
	}

}

export namespace models {
	
	export class AuthStatus {
	    openRouterConfigured: boolean;
	    elevenLabsConfigured: boolean;
	    googleConfigured: boolean;
	
	    static createFrom(source: any = {}) {
	        return new AuthStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.openRouterConfigured = source["openRouterConfigured"];
	        this.elevenLabsConfigured = source["elevenLabsConfigured"];
	        this.googleConfigured = source["googleConfigured"];
	    }
	}
	export class CompanyInfo {
	    slug: string;
	    name: string;
	    problemCount: number;
	    mockEligible: boolean;
	
	    static createFrom(source: any = {}) {
	        return new CompanyInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.slug = source["slug"];
	        this.name = source["name"];
	        this.problemCount = source["problemCount"];
	        this.mockEligible = source["mockEligible"];
	    }
	}
	export class Problem {
	    id: number;
	    title: string;
	    difficulty: string;
	    frequency: number;
	    acceptance: number;
	    url: string;
	    recent: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Problem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.difficulty = source["difficulty"];
	        this.frequency = source["frequency"];
	        this.acceptance = source["acceptance"];
	        this.url = source["url"];
	        this.recent = source["recent"];
	    }
	}
	export class Session {
	    id: string;
	    problemId: string;
	    model: string;
	    // Go type: time
	    startedAt: any;
	    // Go type: time
	    endedAt?: any;
	    problemTitle: string;
	    difficulty: string;
	
	    static createFrom(source: any = {}) {
	        return new Session(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.problemId = source["problemId"];
	        this.model = source["model"];
	        this.startedAt = this.convertValues(source["startedAt"], null);
	        this.endedAt = this.convertValues(source["endedAt"], null);
	        this.problemTitle = source["problemTitle"];
	        this.difficulty = source["difficulty"];
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
	export class CompanySessionStart {
	    session: Session;
	    company: string;
	    opening: string;
	    problems: Problem[];
	
	    static createFrom(source: any = {}) {
	        return new CompanySessionStart(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.session = this.convertValues(source["session"], Session);
	        this.company = source["company"];
	        this.opening = source["opening"];
	        this.problems = this.convertValues(source["problems"], Problem);
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
	export class DebriefRubric {
	    problemSolving: number;
	    coding: number;
	    communication: number;
	    complexity: number;
	    pace: number;
	
	    static createFrom(source: any = {}) {
	        return new DebriefRubric(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.problemSolving = source["problemSolving"];
	        this.coding = source["coding"];
	        this.communication = source["communication"];
	        this.complexity = source["complexity"];
	        this.pace = source["pace"];
	    }
	}
	export class Debrief {
	    verdict: string;
	    summary: string;
	    rubric: DebriefRubric;
	    strengths: string[];
	    improvements: string[];
	
	    static createFrom(source: any = {}) {
	        return new Debrief(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.verdict = source["verdict"];
	        this.summary = source["summary"];
	        this.rubric = this.convertValues(source["rubric"], DebriefRubric);
	        this.strengths = source["strengths"];
	        this.improvements = source["improvements"];
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
	
	export class Message {
	    id: string;
	    sessionId: string;
	    role: string;
	    content: string;
	    hasImage: boolean;
	    // Go type: time
	    createdAt: any;
	
	    static createFrom(source: any = {}) {
	        return new Message(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.sessionId = source["sessionId"];
	        this.role = source["role"];
	        this.content = source["content"];
	        this.hasImage = source["hasImage"];
	        this.createdAt = this.convertValues(source["createdAt"], null);
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
	export class Model {
	    id: string;
	    name: string;
	    description: string;
	    contextLength: number;
	    supportsVision: boolean;
	    isFree: boolean;
	    promptPrice: number;
	    completionPrice: number;
	
	    static createFrom(source: any = {}) {
	        return new Model(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.description = source["description"];
	        this.contextLength = source["contextLength"];
	        this.supportsVision = source["supportsVision"];
	        this.isFree = source["isFree"];
	        this.promptPrice = source["promptPrice"];
	        this.completionPrice = source["completionPrice"];
	    }
	}
	export class Preferences {
	    captureIntervalMs: number;
	    model: string;
	    voiceSpeed: number;
	    ttsProvider: string;
	    voiceId: string;
	    googleVoiceId: string;
	    captureDisplay: number;
	    regionX: number;
	    regionY: number;
	    regionW: number;
	    regionH: number;
	    sessionLimitMinutes: number;
	    softWarningMinutes: number;
	    pushToTalkEnabled: boolean;
	    pushToTalkKey: string;
	    lastCompany: string;
	    lastDifficulty: string;
	
	    static createFrom(source: any = {}) {
	        return new Preferences(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.captureIntervalMs = source["captureIntervalMs"];
	        this.model = source["model"];
	        this.voiceSpeed = source["voiceSpeed"];
	        this.ttsProvider = source["ttsProvider"];
	        this.voiceId = source["voiceId"];
	        this.googleVoiceId = source["googleVoiceId"];
	        this.captureDisplay = source["captureDisplay"];
	        this.regionX = source["regionX"];
	        this.regionY = source["regionY"];
	        this.regionW = source["regionW"];
	        this.regionH = source["regionH"];
	        this.sessionLimitMinutes = source["sessionLimitMinutes"];
	        this.softWarningMinutes = source["softWarningMinutes"];
	        this.pushToTalkEnabled = source["pushToTalkEnabled"];
	        this.pushToTalkKey = source["pushToTalkKey"];
	        this.lastCompany = source["lastCompany"];
	        this.lastDifficulty = source["lastDifficulty"];
	    }
	}
	
	
	export class SessionSummary {
	    id: string;
	    problemTitle: string;
	    difficulty: string;
	    model: string;
	    // Go type: time
	    startedAt: any;
	    // Go type: time
	    endedAt?: any;
	    messageCount: number;
	    company: string;
	    mode: string;
	    debrief?: Debrief;
	
	    static createFrom(source: any = {}) {
	        return new SessionSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.problemTitle = source["problemTitle"];
	        this.difficulty = source["difficulty"];
	        this.model = source["model"];
	        this.startedAt = this.convertValues(source["startedAt"], null);
	        this.endedAt = this.convertValues(source["endedAt"], null);
	        this.messageCount = source["messageCount"];
	        this.company = source["company"];
	        this.mode = source["mode"];
	        this.debrief = this.convertValues(source["debrief"], Debrief);
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
	export class UpdateInfo {
	    available: boolean;
	    currentVersion: string;
	    latestVersion: string;
	    releaseUrl: string;
	    downloadUrl: string;
	    notes: string;
	
	    static createFrom(source: any = {}) {
	        return new UpdateInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.available = source["available"];
	        this.currentVersion = source["currentVersion"];
	        this.latestVersion = source["latestVersion"];
	        this.releaseUrl = source["releaseUrl"];
	        this.downloadUrl = source["downloadUrl"];
	        this.notes = source["notes"];
	    }
	}
	export class Voice {
	    id: string;
	    name: string;
	    category: string;
	    previewUrl: string;
	
	    static createFrom(source: any = {}) {
	        return new Voice(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.category = source["category"];
	        this.previewUrl = source["previewUrl"];
	    }
	}

}

