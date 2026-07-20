pragma solidity ^0.4.20;


library strings {
    struct slice {
        uint _len;
        uint _ptr;
    }

    function memcpy(uint dest, uint src, uint len) private pure {
        for(; len >= 32; len -= 32) {
            assembly { mstore(dest, mload(src)) }
            dest += 32; src += 32;
        }
        uint mask = 256 ** (32 - len) - 1;
        assembly {
            let srcpart := and(mload(src), not(mask))
            let destpart := and(mload(dest), mask)
            mstore(dest, or(destpart, srcpart))
        }
    }

    function toSlice(string self) internal pure returns (slice) {
        uint ptr;
        assembly { ptr := add(self, 0x20) }
        return slice(bytes(self).length, ptr);
    }

    function len(bytes32 self) internal pure returns (uint) {
        uint ret;
        if (self == 0) return 0;
        if (self & 0xffffffffffffffffffffffffffffffff == 0) { ret += 16; self = bytes32(uint(self) / 0x100000000000000000000000000000000); }
        if (self & 0xffffffffffffffff == 0) { ret += 8; self = bytes32(uint(self) / 0x10000000000000000); }
        if (self & 0xffffffff == 0) { ret += 4; self = bytes32(uint(self) / 0x100000000); }
        if (self & 0xffff == 0) { ret += 2; self = bytes32(uint(self) / 0x10000); }
        if (self & 0xff == 0) { ret += 1; }
        return 32 - ret;
    }

    function toSliceB32(bytes32 self) internal pure returns (slice ret) {
        assembly {
            let ptr := mload(0x40)
            mstore(0x40, add(ptr, 0x20))
            mstore(ptr, self)
            mstore(add(ret, 0x20), ptr)
        }
        ret._len = len(self);
    }

    function copy(slice self) internal pure returns (slice) {
        return slice(self._len, self._ptr);
    }

    function toString(slice self) internal pure returns (string) {
        string memory ret = new string(self._len);
        uint retptr;
        assembly { retptr := add(ret, 32) }
        memcpy(retptr, self._ptr, self._len);
        return ret;
    }

    function len(slice self) internal pure returns (uint l) {
        uint ptr = self._ptr - 31;
        uint end = ptr + self._len;
        for (l = 0; ptr < end; l++) {
            uint8 b;
            assembly { b := and(mload(ptr), 0xFF) }
            if (b < 0x80) { ptr += 1; }
            else if(b < 0xE0) { ptr += 2; }
            else if(b < 0xF0) { ptr += 3; }
            else if(b < 0xF8) { ptr += 4; }
            else if(b < 0xFC) { ptr += 5; }
            else { ptr += 6; }
        }
    }

    function empty(slice self) internal pure returns (bool) {
        return self._len == 0;
    }

    function compare(slice self, slice other) internal pure returns (int) {
        uint shortest = self._len;
        if (other._len < self._len) shortest = other._len;
        uint selfptr = self._ptr;
        uint otherptr = other._ptr;
        for (uint idx = 0; idx < shortest; idx += 32) {
            uint a; uint b;
            assembly {
                a := mload(selfptr)
                b := mload(otherptr)
            }
            if (a != b) {
                uint256 mask = uint256(-1);
                if(shortest < 32) { mask = ~(2 ** (8 * (32 - shortest + idx)) - 1); }
                uint256 diff = (a & mask) - (b & mask);
                if (diff != 0) return int(diff);
            }
            selfptr += 32; otherptr += 32;
        }
        return int(self._len) - int(other._len);
    }

    function equals(slice self, slice other) internal pure returns (bool) {
        return compare(self, other) == 0;
    }

    function nextRune(slice self, slice rune) internal pure returns (slice) {
        rune._ptr = self._ptr;
        if (self._len == 0) { rune._len = 0; return rune; }
        uint l; uint b;
        assembly { b := and(mload(sub(mload(add(self, 32)), 31)), 0xFF) }
        if (b < 0x80) { l = 1; }
        else if(b < 0xE0) { l = 2; }
        else if(b < 0xF0) { l = 3; }
        else { l = 4; }
        if (l > self._len) { rune._len = self._len; self._ptr += self._len; self._len = 0; return rune; }
        self._ptr += l; self._len -= l; rune._len = l;
        return rune;
    }

    function nextRune(slice self) internal pure returns (slice ret) {
        nextRune(self, ret);
    }

    function ord(slice self) internal pure returns (uint ret) {
        if (self._len == 0) { return 0; }
        uint word; uint length; uint divisor = 2 ** 248;
        assembly { word:= mload(mload(add(self, 32))) }
        uint b = word / divisor;
        if (b < 0x80) { ret = b; length = 1; }
        else if(b < 0xE0) { ret = b & 0x1F; length = 2; }
        else if(b < 0xF0) { ret = b & 0x0F; length = 3; }
        else { ret = b & 0x07; length = 4; }
        if (length > self._len) { return 0; }
        for (uint i = 1; i < length; i++) {
            divisor = divisor / 256;
            b = (word / divisor) & 0xFF;
            if (b & 0xC0 != 0x80) { return 0; }
            ret = (ret * 64) | (b & 0x3F);
        }
        return ret;
    }

    function keccak(slice self) internal pure returns (bytes32 ret) {
        assembly { ret := keccak256(mload(add(self, 32)), mload(self)) }
    }

    function startsWith(slice self, slice needle) internal pure returns (bool) {
        if (self._len < needle._len) { return false; }
        if (self._ptr == needle._ptr) { return true; }
        bool equal;
        assembly {
            let length := mload(needle)
            let selfptr := mload(add(self, 0x20))
            let needleptr := mload(add(needle, 0x20))
            equal := eq(keccak256(selfptr, length), keccak256(needleptr, length))
        }
        return equal;
    }

    function beyond(slice self, slice needle) internal pure returns (slice) {
        if (self._len < needle._len) { return self; }
        bool equal = true;
        if (self._ptr != needle._ptr) {
            assembly {
                let length := mload(needle)
                let selfptr := mload(add(self, 0x20))
                let needleptr := mload(add(needle, 0x20))
                equal := eq(keccak256(selfptr, length), keccak256(needleptr, length))
            }
        }
        if (equal) { self._len -= needle._len; self._ptr += needle._len; }
        return self;
    }

    function endsWith(slice self, slice needle) internal pure returns (bool) {
        if (self._len < needle._len) { return false; }
        uint selfptr = self._ptr + self._len - needle._len;
        if (selfptr == needle._ptr) { return true; }
        bool equal;
        assembly {
            let length := mload(needle)
            let needleptr := mload(add(needle, 0x20))
            equal := eq(keccak256(selfptr, length), keccak256(needleptr, length))
        }
        return equal;
    }

    function until(slice self, slice needle) internal pure returns (slice) {
        if (self._len < needle._len) { return self; }
        uint selfptr = self._ptr + self._len - needle._len;
        bool equal = true;
        if (selfptr != needle._ptr) {
            assembly {
                let length := mload(needle)
                let needleptr := mload(add(needle, 0x20))
                equal := eq(keccak256(selfptr, length), keccak256(needleptr, length))
            }
        }
        if (equal) { self._len -= needle._len; }
        return self;
    }

    event log_bytemask(bytes32 mask);

    function findPtr(uint selflen, uint selfptr, uint needlelen, uint needleptr) private pure returns (uint) {
        uint ptr = selfptr;
        uint idx;
        if (needlelen <= selflen) {
            if (needlelen <= 32) {
                bytes32 mask = bytes32(~(2 ** (8 * (32 - needlelen)) - 1));
                bytes32 needledata;
                assembly { needledata := and(mload(needleptr), mask) }
                uint end = selfptr + selflen - needlelen;
                bytes32 ptrdata;
                assembly { ptrdata := and(mload(ptr), mask) }
                while (ptrdata != needledata) {
                    if (ptr >= end) return selfptr + selflen;
                    ptr++;
                    assembly { ptrdata := and(mload(ptr), mask) }
                }
                return ptr;
            } else {
                bytes32 hash;
                assembly { hash := keccak256(needleptr, needlelen) }
                for (idx = 0; idx <= selflen - needlelen; idx++) {
                    bytes32 testHash;
                    assembly { testHash := keccak256(ptr, needlelen) }
                    if (hash == testHash) return ptr;
                    ptr += 1;
                }
            }
        }
        return selfptr + selflen;
    }

    function rfindPtr(uint selflen, uint selfptr, uint needlelen, uint needleptr) private pure returns (uint) {
        uint ptr;
        if (needlelen <= selflen) {
            if (needlelen <= 32) {
                bytes32 mask = bytes32(~(2 ** (8 * (32 - needlelen)) - 1));
                bytes32 needledata;
                assembly { needledata := and(mload(needleptr), mask) }
                ptr = selfptr + selflen - needlelen;
                bytes32 ptrdata;
                assembly { ptrdata := and(mload(ptr), mask) }
                while (ptrdata != needledata) {
                    if (ptr <= selfptr) return selfptr;
                    ptr--;
                    assembly { ptrdata := and(mload(ptr), mask) }
                }
                return ptr + needlelen;
            } else {
                bytes32 hash;
                assembly { hash := keccak256(needleptr, needlelen) }
                ptr = selfptr + (selflen - needlelen);
                while (ptr >= selfptr) {
                    bytes32 testHash;
                    assembly { testHash := keccak256(ptr, needlelen) }
                    if (hash == testHash) return ptr + needlelen;
                    ptr -= 1;
                }
            }
        }
        return selfptr;
    }

    function find(slice self, slice needle) internal pure returns (slice) {
        uint ptr = findPtr(self._len, self._ptr, needle._len, needle._ptr);
        self._len -= ptr - self._ptr;
        self._ptr = ptr;
        return self;
    }

    function rfind(slice self, slice needle) internal pure returns (slice) {
        uint ptr = rfindPtr(self._len, self._ptr, needle._len, needle._ptr);
        self._len = ptr - self._ptr;
        return self;
    }

    function split(slice self, slice needle, slice token) internal pure returns (slice) {
        uint ptr = findPtr(self._len, self._ptr, needle._len, needle._ptr);
        token._ptr = self._ptr;
        token._len = ptr - self._ptr;
        if (ptr == self._ptr + self._len) { self._len = 0; }
        else { self._len -= token._len + needle._len; self._ptr = ptr + needle._len; }
        return token;
    }

    function split(slice self, slice needle) internal pure returns (slice token) {
        split(self, needle, token);
    }

    function rsplit(slice self, slice needle, slice token) internal pure returns (slice) {
        uint ptr = rfindPtr(self._len, self._ptr, needle._len, needle._ptr);
        token._ptr = ptr;
        token._len = self._len - (ptr - self._ptr);
        if (ptr == self._ptr) { self._len = 0; }
        else { self._len -= token._len + needle._len; }
        return token;
    }

    function rsplit(slice self, slice needle) internal pure returns (slice token) {
        rsplit(self, needle, token);
    }

    function count(slice self, slice needle) internal pure returns (uint cnt) {
        uint ptr = findPtr(self._len, self._ptr, needle._len, needle._ptr) + needle._len;
        while (ptr <= self._ptr + self._len) {
            cnt++;
            ptr = findPtr(self._len - (ptr - self._ptr), ptr, needle._len, needle._ptr) + needle._len;
        }
    }

    function contains(slice self, slice needle) internal pure returns (bool) {
        return rfindPtr(self._len, self._ptr, needle._len, needle._ptr) != self._ptr;
    }

    function concat(slice self, slice other) internal pure returns (string) {
        string memory ret = new string(self._len + other._len);
        uint retptr;
        assembly { retptr := add(ret, 32) }
        memcpy(retptr, self._ptr, self._len);
        memcpy(retptr + self._len, other._ptr, other._len);
        return ret;
    }

    function concatS(slice self, slice other) internal pure returns (slice) {
        string memory ret = new string(self._len + other._len);
        uint retptr;
        assembly { retptr := add(ret, 32) }
        memcpy(retptr, self._ptr, self._len);
        memcpy(retptr + self._len, other._ptr, other._len);
        return slice(self._len + other._len, retptr);
    }

    function concatA(slice self, slice[] parts) internal pure returns (string) {
        if (parts.length == 0) return "";
        uint length = self._len;
        uint i;
        for(i = 0; i < parts.length; i++) length += parts[i]._len;
        string memory ret = new string(length);
        uint retptr;
        assembly { retptr := add(ret, 32) }
        memcpy(retptr, self._ptr, self._len);
        retptr += self._len;
        for(i = 0; i < parts.length; i++) {
            memcpy(retptr, parts[i]._ptr, parts[i]._len);
            retptr += parts[i]._len;
        }
        return ret;
    }

    function join(slice self, slice[] parts) internal pure returns (string) {
        if (parts.length == 0) return "";
        uint length = self._len * (parts.length - 1);
        uint i;
        for(i = 0; i < parts.length; i++) length += parts[i]._len;
        string memory ret = new string(length);
        uint retptr;
        assembly { retptr := add(ret, 32) }
        for(i = 0; i < parts.length; i++) {
            memcpy(retptr, parts[i]._ptr, parts[i]._len);
            retptr += parts[i]._len;
            if (i < parts.length - 1) {
                memcpy(retptr, self._ptr, self._len);
                retptr += self._len;
            }
        }
        return ret;
    }
}

library utils{
    function ecrecoverSig(bytes32 hash, bytes signature) internal pure returns(identity){
        bytes32 r = bytesToBytes32(sliceSig(signature, 0, 32));
        bytes32 s = bytesToBytes32(sliceSig(signature, 32, 32));
        byte v1 = sliceSig(signature, 64, 1)[0];
        uint8 v = uint8(v1) + 27;
        return ecrecover(hash, v, r, s);
    }

    function sliceSig(bytes memory data, uint start, uint len) internal pure returns(bytes){
        bytes memory b = new bytes(len);
        for(uint i = 0; i < len; i++){ b[i] = data[i + start]; }
        return b;
    }

    function bytesToBytes32(bytes memory source) internal pure returns (bytes32 result) {
        assembly { result := mload(add(source, 32)) }
    }

    function bytes32ToString(bytes32 x) internal pure returns (string) {
        bytes memory bytesString = new bytes(32);
        uint charCount = 0;
        uint j;
        for (j = 0; j < 32; j++) {
            byte char = byte(bytes32(uint(x) * 2 ** (8 * j)));
            if (char != 0) { bytesString[charCount] = char; charCount++; }
        }
        bytes memory bytesStringTrimmed = new bytes(charCount);
        for (j = 0; j < charCount; j++) { bytesStringTrimmed[j] = bytesString[j]; }
        return string(bytesStringTrimmed);
    }

    function bytes20ToString(bytes20 x) internal pure returns (string) {
        bytes memory bytesString = new bytes(20);
        uint charCount = 0;
        for (uint j = 0; j < 20; j++) {
            byte char = byte(bytes20(uint(x) * 2 ** (8 * j)));
            if (char != 0) { bytesString[charCount] = char; charCount++; }
        }
        bytes memory bytesStringTrimmed = new bytes(charCount);
        for (j = 0; j < charCount; j++) { bytesStringTrimmed[j] = bytesString[j]; }
        return string(bytesStringTrimmed);
    }

    function bytesToHexString(bytes memory bs) internal pure returns(string) {
        bytes memory tempBytes = new bytes(bs.length * 2);
        uint len = bs.length;
        for (uint i = 0; i < len; i++) {
            byte b = bs[i];
            byte nb = (b & 0xf0) >> 4;
            tempBytes[2 * i] = nb > 0x09 ? byte((uint8(nb) + 0x37)) : (nb | 0x30);
            nb = (b & 0x0f);
            tempBytes[2 * i + 1] = nb > 0x09 ? byte((uint8(nb) + 0x37)) : (nb | 0x30);
        }
        return string(tempBytes);
    }

    function bytes20ToHexString(bytes20 bs) internal pure returns(string) {
        bytes memory tempBytes = new bytes(bs.length * 2);
        uint len = bs.length;
        for (uint i = 0; i < len; i++) {
            byte b = bs[i];
            byte nb = (b & 0xf0) >> 4;
            tempBytes[2 * i] = nb > 0x09 ? byte((uint8(nb) + 0x37)) : (nb | 0x30);
            nb = (b & 0x0f);
            tempBytes[2 * i + 1] = nb > 0x09 ? byte((uint8(nb) + 0x37)) : (nb | 0x30);
        }
        return string(tempBytes);
    }

    function bytesToHexString(bytes32 bs) internal pure returns(string) {
        bytes memory tempBytes = new bytes(bs.length * 2);
        uint len = bs.length;
        for (uint i = 0; i < len; i++) {
            byte b = bs[i];
            byte nb = (b & 0xf0) >> 4;
            tempBytes[2 * i] = nb > 0x09 ? byte((uint8(nb) + 0x37)) : (nb | 0x30);
            nb = (b & 0x0f);
            tempBytes[2 * i + 1] = nb > 0x09 ? byte((uint8(nb) + 0x37)) : (nb | 0x30);
        }
        return string(tempBytes);
    }

    function uintToString(uint i) internal pure returns (string){
        if (i == 0) return "0";
        uint j = i; uint length;
        while (j != 0){ length++; j /= 10; }
        bytes memory bstr = new bytes(length);
        uint k = length - 1;
        while (i != 0){ bstr[k--] = byte(uint8(48) + uint8(i % 10)); i /= 10; }
        return string(bstr);
    }

    function bytes32ToBytes(bytes32 data) internal pure returns (bytes) {
        bytes memory result = new bytes(32);
        assembly { mstore(add(result, 32), data) }
        return result;
    }

    function stringToBytes32(string memory source) internal pure returns (bytes32 result) {
        assembly { result := mload(add(source, 32)) }
    }

    function stringToBytes20(string memory source) internal pure returns (bytes20 result) {
        assembly { result := mload(add(source, 20)) }
    }

    function bytes32ArrayToString(bytes32[] data) internal pure returns (string) {
        bytes memory bytesString = new bytes(data.length * 32);
        uint urlLength;
        for (uint i = 0; i< data.length; i++) {
            for (uint j = 0; j < 32; j++) {
                byte char = byte(bytes32(uint(data[i]) * 2 ** (8 * j)));
                if (char != 0) { bytesString[urlLength] = char; urlLength += 1; }
            }
        }
        bytes memory bytesStringTrimmed = new bytes(urlLength);
        for (i = 0; i < urlLength; i++) { bytesStringTrimmed[i] = bytesString[i]; }
        return string(bytesStringTrimmed);
    }

    function stringToUint(string s) internal pure returns (uint) {
        bytes memory b = bytes(s);
        uint result = 0;
        for (uint i = 0; i < b.length; i++) {
            if (b[i] >= 48 && b[i] <= 57) { result = result * 10 + (uint(b[i]) - 48); }
        }
        return result;
    }

    function compare_string(string a, string b) internal returns (bool) {
        if (bytes(a).length != bytes(b).length) { return false; }
        else { return keccak256(a) == keccak256(b); }
    }
}

contract OrderEvidence {

    event LOG_STRING(string);
    using strings for *;
    using utils for *;

    struct Order{
        string tradeNo;
        string userId;
        string moneyFen;
        string planId;
        string provider;
        string payTime;
        string status;
        string dataHash;
        string bizType;
        string preTxHash;
        string deliveredAt;   // 段2b: 发货/履约时刻 ISO8601
        string deliveryHash;  // 段2b: 发货防篡改指纹 SHA-256(canonicalJSON(tradeNo,deliveredAt))
    }

    mapping (string => Order) orders;

    identity public owner;
    function OrderEvidence() public { owner = msg.sender; }
    modifier onlyOwner(){ require(msg.sender == owner); _; }

    // 浏览器展示日志:JSON 由链下构建,直接发射
    function logOrder(string tradeNo, string browserJson) public onlyOwner {
        require(!orders[tradeNo].tradeNo.compare_string(""));
        emit LOG_STRING(browserJson);
    }

    // 第一步:写入前5个字段
    function insertOrder(
        string tradeNo,
        string userId,
        string moneyFen,
        string planId,
        string provider
    ) public onlyOwner {
        require(orders[tradeNo].tradeNo.compare_string(""));
        orders[tradeNo].tradeNo   = tradeNo;
        orders[tradeNo].userId    = userId;
        orders[tradeNo].moneyFen  = moneyFen;
        orders[tradeNo].planId    = planId;
        orders[tradeNo].provider  = provider;
    }

    // 第二步:写入剩余字段
    function completeOrder(
        string tradeNo,
        string payTime,
        string status,
        string dataHash,
        string bizType
    ) public onlyOwner {
        require(!orders[tradeNo].tradeNo.compare_string(""));
        orders[tradeNo].payTime   = payTime;
        orders[tradeNo].status    = status;
        orders[tradeNo].dataHash  = dataHash;
        orders[tradeNo].bizType   = bizType;
        orders[tradeNo].preTxHash = "";
    }

    // 段2b 第三步: 发货/履约存证。订单须已存在(insertOrder 写入)。
    // 仅 emit LOG_STRING(浏览器易读 JSON), deliveredAt/deliveryHash 存入 struct 备查。
    function deliverOrder(string tradeNo, string browserJson, string deliveredAt, string deliveryHash) public onlyOwner {
        require(!orders[tradeNo].tradeNo.compare_string(""));
        orders[tradeNo].deliveredAt  = deliveredAt;
        orders[tradeNo].deliveryHash = deliveryHash;
        emit LOG_STRING(browserJson);
    }

    function getTradeNo(string tradeNo) public constant returns (string) {
        return orders[tradeNo].tradeNo;
    }
    function getUserId(string tradeNo) public constant returns (string) {
        return orders[tradeNo].userId;
    }
    function getMoneyFen(string tradeNo) public constant returns (string) {
        return orders[tradeNo].moneyFen;
    }
    function getPlanId(string tradeNo) public constant returns (string) {
        return orders[tradeNo].planId;
    }
    function getProvider(string tradeNo) public constant returns (string) {
        return orders[tradeNo].provider;
    }
    function getPayTime(string tradeNo) public constant returns (string) {
        return orders[tradeNo].payTime;
    }
    function getStatus(string tradeNo) public constant returns (string) {
        return orders[tradeNo].status;
    }
    function getDataHash(string tradeNo) public constant returns (string) {
        return orders[tradeNo].dataHash;
    }
    function getBizType(string tradeNo) public constant returns (string) {
        return orders[tradeNo].bizType;
    }
    function getDeliveredAt(string tradeNo) public constant returns (string) {
        return orders[tradeNo].deliveredAt;
    }
    function getDeliveryHash(string tradeNo) public constant returns (string) {
        return orders[tradeNo].deliveryHash;
    }

    // 诊断:返回各字段长度,确认数据是否独立存储
    function debugLengths(string tradeNo) public constant returns (
        uint, uint, uint, uint, uint
    ){
        return (
            bytes(orders[tradeNo].tradeNo).length,
            bytes(orders[tradeNo].userId).length,
            bytes(orders[tradeNo].moneyFen).length,
            bytes(orders[tradeNo].payTime).length,
            bytes(orders[tradeNo].status).length
        );
    }
}
