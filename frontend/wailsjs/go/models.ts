export namespace main {
	
	export class FlashFile {
	    path: string;
	    offset: number;
	
	    static createFrom(source: any = {}) {
	        return new FlashFile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.offset = source["offset"];
	    }
	}

}

