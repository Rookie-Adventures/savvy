pragma solidity ^0.4.20;


library strings {
    struct slice {
        uint _len;
        uint _ptr;
    }

    function memcpy(uint dest, uint src, uint len) private pure {
        // Copy word-length chunks while possible
        for(; len >= 32; len -= 32) {
            assembly {
                mstore(dest, mload(src))
            }
            dest += 32;
            src += 32;
        }

        // Copy remaining bytes
        uint mask = 256 ** (32 - len) - 1;
        assembly {
            let srcpart := and(mload(src), not(mask))
            let destpart := and(mload(dest), mask)
            mstore(dest, or(destpart, srcpart))
        }
    }

    /*
     * @dev Returns a slice containing the entire string.
     * @param self The string to make a slice from.
     * @return A newly allocated slice containing the entire string.
     */
    function toSlice(string self) internal pure returns (slice) {
        uint ptr;
        assembly {
            ptr := add(self, 0x20)
        }
        return slice(bytes(self).length, ptr);
    }

    /*
     * @dev Returns the length of a null-terminated bytes32 string.
     * @param self The value to find the length of.
     * @return The length of the string, from 0 to 32.
     */
    function len(bytes32 self) internal pure returns (uint) {
        uint ret;
        if (self == 0)
            return 0;
        if (self & 0xffffffffffffffffffffffffffffffff == 0) {
            ret += 16;
            self = bytes32(uint(self) / 0x100000000000000000000000000000000);
        }
        if (self & 0xffffffffffffffff == 0) {
            ret += 8;
            self = bytes32(uint(self) / 0x10000000000000000);
        }
        if (self & 0xffffffff == 0) {
            ret += 4;
            self = bytes32(uint(self) / 0x100000000);
        }
        if (self & 0xffff == 0) {
            ret += 2;
            self = bytes32(uint(self) / 0x10000);
        }
        if (self & 0xff == 0) {
            ret += 1;
        }
        return 32 - ret;
    }

    /*
     * @dev Returns a slice containing the entire bytes32, interpreted as a
     *      null-terminated utf-8 string.
     * @param self The bytes32 value to convert to a slice.
     * @return A new slice containing the value of the input argument up to the
     *         first null.
     */
    function toSliceB32(bytes32 self) internal pure returns (slice ret) {
        // Allocate space for `self` in memory, copy it there, and point ret at it
        assembly {
            let ptr := mload(0x40)
            mstore(0x40, add(ptr, 0x20))
            mstore(ptr, self)
            mstore(add(ret, 0x20), ptr)
        }
        ret._len = len(self);
    }

    /*
     * @dev Returns a new slice containing the same data as the current slice.
     * @param self The slice to copy.
     * @return A new slice containing the same data as `self`.
     */
    function copy(slice self) internal pure returns (slice) {
        return slice(self._len, self._ptr);
    }

    /*
     * @dev Copies a slice to a new string.
     * @param self The slice to copy.
     * @return A newly allocated string containing the slice's text.
     */
    function toString(slice self) internal pure returns (string) {
        string memory ret = new string(self._len);
        uint retptr;
        assembly { retptr := add(ret, 32) }

        memcpy(retptr, self._ptr, self._len);
        return ret;
    }

    /*
     * @dev Returns the length in runes of the slice. Note that this operation
     *      takes time proportional to the length of the slice; avoid using it
     *      in loops, and call `slice.empty()` if you only need to know whether
     *      the slice is empty or not.
     * @param self The slice to operate on.
     * @return The length of the slice in runes.
     */
    function len(slice self) internal pure returns (uint l) {
        // Starting at ptr-31 means the LSB will be the byte we care about
        uint ptr = self._ptr - 31;
        uint end = ptr + self._len;
        for (l = 0; ptr < end; l++) {
            uint8 b;
            assembly { b := and(mload(ptr), 0xFF) }
            if (b < 0x80) {
                ptr += 1;
            } else if(b < 0xE0) {
                ptr += 2;
            } else if(b < 0xF0) {
                ptr += 3;
            } else if(b < 0xF8) {
                ptr += 4;
            } else if(b < 0xFC) {
                ptr += 5;
            } else {
                ptr += 6;
            }
        }
    }

    /*
     * @dev Returns true if the slice is empty (has a length of 0).
     * @param self The slice to operate on.
     * @return True if the slice is empty, False otherwise.
     */
    function empty(slice self) internal pure returns (bool) {
        return self._len == 0;
    }

    /*
     * @dev Returns a positive number if `other` comes lexicographically after
     *      `self`, a negative number if it comes before, or zero if the
     *      contents of the two slices are equal. Comparison is done per-rune,
     *      on unicode codepoints.
     * @param self The first slice to compare.
     * @param other The second slice to compare.
     * @return The result of the comparison.
     */
    function compare(slice self, slice other) internal pure returns (int) {
        uint shortest = self._len;
        if (other._len < self._len)
            shortest = other._len;

        uint selfptr = self._ptr;
        uint otherptr = other._ptr;
        for (uint idx = 0; idx < shortest; idx += 32) {
            uint a;
            uint b;
            assembly {
                a := mload(selfptr)
                b := mload(otherptr)
            }
            if (a != b) {
                // Mask out irrelevant bytes and check again
                uint256 mask = uint256(-1); // 0xffff...
                if(shortest < 32) {
                  mask = ~(2 ** (8 * (32 - shortest + idx)) - 1);
                }
                uint256 diff = (a & mask) - (b & mask);
                if (diff != 0)
                    return int(diff);
            }
            selfptr += 32;
            otherptr += 32;
        }
        return int(self._len) - int(other._len);
    }

    /*
     * @dev Returns true if the two slices contain the same text.
     * @param self The first slice to compare.
     * @param self The second slice to compare.
     * @return True if the slices are equal, false otherwise.
     */
    function equals(slice self, slice other) internal pure returns (bool) {
        return compare(self, other) == 0;
    }

    /*
     * @dev Extracts the first rune in the slice into `rune`, advancing the
     *      slice to point to the next rune and returning `self`.
     * @param self The slice to operate on.
     * @param rune The slice that will contain the first rune.
     * @return `rune`.
     */
    function nextRune(slice self, slice rune) internal pure returns (slice) {
        rune._ptr = self._ptr;

        if (self._len == 0) {
            rune._len = 0;
            return rune;
        }

        uint l;
        uint b;
        // Load the first byte of the rune into the LSBs of b
        assembly { b := and(mload(sub(mload(add(self, 32)), 31)), 0xFF) }
        if (b < 0x80) {
            l = 1;
        } else if(b < 0xE0) {
            l = 2;
        } else if(b < 0xF0) {
            l = 3;
        } else {
            l = 4;
        }

        // Check for truncated codepoints
        if (l > self._len) {
            rune._len = self._len;
            self._ptr += self._len;
            self._len = 0;
            return rune;
        }

        self._ptr += l;
        self._len -= l;
        rune._len = l;
        return rune;
    }

    /*
     * @dev Returns the first rune in the slice, advancing the slice to point
     *      to the next rune.
     * @param self The slice to operate on.
     * @return A slice containing only the first rune from `self`.
     */
    function nextRune(slice self) internal pure returns (slice ret) {
        nextRune(self, ret);
    }

    /*
     * @dev Returns the number of the first codepoint in the slice.
     * @param self The slice to operate on.
     * @return The number of the first codepoint in the slice.
     */
    function ord(slice self) internal pure returns (uint ret) {
        if (self._len == 0) {
            return 0;
        }

        uint word;
        uint length;
        uint divisor = 2 ** 248;

        // Load the rune into the MSBs of b
        assembly { word:= mload(mload(add(self, 32))) }
        uint b = word / divisor;
        if (b < 0x80) {
            ret = b;
            length = 1;
        } else if(b < 0xE0) {
            ret = b & 0x1F;
            length = 2;
        } else if(b < 0xF0) {
            ret = b & 0x0F;
            length = 3;
        } else {
            ret = b & 0x07;
            length = 4;
        }

        // Check for truncated codepoints
        if (length > self._len) {
            return 0;
        }

        for (uint i = 1; i < length; i++) {
            divisor = divisor / 256;
            b = (word / divisor) & 0xFF;
            if (b & 0xC0 != 0x80) {
                // Invalid UTF-8 sequence
                return 0;
            }
            ret = (ret * 64) | (b & 0x3F);
        }

        return ret;
    }

    /*
     * @dev Returns the keccak-256 hash of the slice.
     * @param self The slice to hash.
     * @return The hash of the slice.
     */
    function keccak(slice self) internal pure returns (bytes32 ret) {
        assembly {
            ret := keccak256(mload(add(self, 32)), mload(self))
        }
    }

    /*
     * @dev Returns true if `self` starts with `needle`.
     * @param self The slice to operate on.
     * @param needle The slice to search for.
     * @return True if the slice starts with the provided text, false otherwise.
     */
    function startsWith(slice self, slice needle) internal pure returns (bool) {
        if (self._len < needle._len) {
            return false;
        }

        if (self._ptr == needle._ptr) {
            return true;
        }

        bool equal;
        assembly {
            let length := mload(needle)
            let selfptr := mload(add(self, 0x20))
            let needleptr := mload(add(needle, 0x20))
            equal := eq(keccak256(selfptr, length), keccak256(needleptr, length))
        }
        return equal;
    }

    /*
     * @dev If `self` starts with `needle`, `needle` is removed from the
     *      beginning of `self`. Otherwise, `self` is unmodified.
     * @param self The slice to operate on.
     * @param needle The slice to search for.
     * @return `self`
     */
    function beyond(slice self, slice needle) internal pure returns (slice) {
        if (self._len < needle._len) {
            return self;
        }

        bool equal = true;
        if (self._ptr != needle._ptr) {
            assembly {
                let length := mload(needle)
                let selfptr := mload(add(self, 0x20))
                let needleptr := mload(add(needle, 0x20))
                equal := eq(keccak256(selfptr, length), keccak256(needleptr, length))
            }
        }

        if (equal) {
            self._len -= needle._len;
            self._ptr += needle._len;
        }

        return self;
    }

    /*
     * @dev Returns true if the slice ends with `needle`.
     * @param self The slice to operate on.
     * @param needle The slice to search for.
     * @return True if the slice starts with the provided text, false otherwise.
     */
    function endsWith(slice self, slice needle) internal pure returns (bool) {
        if (self._len < needle._len) {
            return false;
        }

        uint selfptr = self._ptr + self._len - needle._len;

        if (selfptr == needle._ptr) {
            return true;
        }

        bool equal;
        assembly {
            let length := mload(needle)
            let needleptr := mload(add(needle, 0x20))
            equal := eq(keccak256(selfptr, length), keccak256(needleptr, length))
        }

        return equal;
    }

    /*
     * @dev If `self` ends with `needle`, `needle` is removed from the
     *      end of `self`. Otherwise, `self` is unmodified.
     * @param self The slice to operate on.
     * @param needle The slice to search for.
     * @return `self`
     */
    function until(slice self, slice needle) internal pure returns (slice) {
        if (self._len < needle._len) {
            return self;
        }

        uint selfptr = self._ptr + self._len - needle._len;
        bool equal = true;
        if (selfptr != needle._ptr) {
            assembly {
                let length := mload(needle)
                let needleptr := mload(add(needle, 0x20))
                equal := eq(keccak256(selfptr, length), keccak256(needleptr, length))
            }
        }

        if (equal) {
            self._len -= needle._len;
        }

        return self;
    }

    event log_bytemask(bytes32 mask);

    // Returns the memory address of the first byte of the first occurrence of
    // `needle` in `self`, or the first byte after `self` if not found.
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
                    if (ptr >= end)
                        return selfptr + selflen;
                    ptr++;
                    assembly { ptrdata := and(mload(ptr), mask) }
                }
                return ptr;
            } else {
                // For long needles, use hashing
                bytes32 hash;
                assembly { hash := keccak256(needleptr, needlelen) }

                for (idx = 0; idx <= selflen - needlelen; idx++) {
                    bytes32 testHash;
                    assembly { testHash := keccak256(ptr, needlelen) }
                    if (hash == testHash)
                        return ptr;
                    ptr += 1;
                }
            }
        }
        return selfptr + selflen;
    }

    // Returns the memory address of the first byte after the last occurrence of
    // `needle` in `self`, or the address of `self` if not found.
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
                    if (ptr <= selfptr)
                        return selfptr;
                    ptr--;
                    assembly { ptrdata := and(mload(ptr), mask) }
                }
                return ptr + needlelen;
            } else {
                // For long needles, use hashing
                bytes32 hash;
                assembly { hash := keccak256(needleptr, needlelen) }
                ptr = selfptr + (selflen - needlelen);
                while (ptr >= selfptr) {
                    bytes32 testHash;
                    assembly { testHash := keccak256(ptr, needlelen) }
                    if (hash == testHash)
                        return ptr + needlelen;
                    ptr -= 1;
                }
            }
        }
        return selfptr;
    }

    /*
     * @dev Modifies `self` to contain everything from the first occurrence of
     *      `needle` to the end of the slice. `self` is set to the empty slice
     *      if `needle` is not found.
     * @param self The slice to search and modify.
     * @param needle The text to search for.
     * @return `self`.
     */
    function find(slice self, slice needle) internal pure returns (slice) {
        uint ptr = findPtr(self._len, self._ptr, needle._len, needle._ptr);
        self._len -= ptr - self._ptr;
        self._ptr = ptr;
        return self;
    }

    /*
     * @dev Modifies `self` to contain the part of the string from the start of
     *      `self` to the end of the first occurrence of `needle`. If `needle`
     *      is not found, `self` is set to the empty slice.
     * @param self The slice to search and modify.
     * @param needle The text to search for.
     * @return `self`.
     */
    function rfind(slice self, slice needle) internal pure returns (slice) {
        uint ptr = rfindPtr(self._len, self._ptr, needle._len, needle._ptr);
        self._len = ptr - self._ptr;
        return self;
    }

    /*
     * @dev Splits the slice, setting `self` to everything after the first
     *      occurrence of `needle`, and `token` to everything before it. If
     *      `needle` does not occur in `self`, `self` is set to the empty slice,
     *      and `token` is set to the entirety of `self`.
     * @param self The slice to split.
     * @param needle The text to search for in `self`.
     * @param token An output parameter to which the first token is written.
     * @return `token`.
     */
    function split(slice self, slice needle, slice token) internal pure returns (slice) {
        uint ptr = findPtr(self._len, self._ptr, needle._len, needle._ptr);
        token._ptr = self._ptr;
        token._len = ptr - self._ptr;
        if (ptr == self._ptr + self._len) {
            // Not found
            self._len = 0;
        } else {
            self._len -= token._len + needle._len;
            self._ptr = ptr + needle._len;
        }
        return token;
    }

    /*
     * @dev Splits the slice, setting `self` to everything after the first
     *      occurrence of `needle`, and returning everything before it. If
     *      `needle` does not occur in `self`, `self` is set to the empty slice,
     *      and the entirety of `self` is returned.
     * @param self The slice to split.
     * @param needle The text to search for in `self`.
     * @return The part of `self` up to the first occurrence of `delim`.
     */
    function split(slice self, slice needle) internal pure returns (slice token) {
        split(self, needle, token);
    }

    /*
     * @dev Splits the slice, setting `self` to everything before the last
     *      occurrence of `needle`, and `token` to everything after it. If
     *      `needle` does not occur in `self`, `self` is set to the empty slice,
     *      and `token` is set to the entirety of `self`.
     * @param self The slice to split.
     * @param needle The text to search for in `self`.
     * @param token An output parameter to which the first token is written.
     * @return `token`.
     */
    function rsplit(slice self, slice needle, slice token) internal pure returns (slice) {
        uint ptr = rfindPtr(self._len, self._ptr, needle._len, needle._ptr);
        token._ptr = ptr;
        token._len = self._len - (ptr - self._ptr);
        if (ptr == self._ptr) {
            // Not found
            self._len = 0;
        } else {
            self._len -= token._len + needle._len;
        }
        return token;
    }

    /*
     * @dev Splits the slice, setting `self` to everything before the last
     *      occurrence of `needle`, and returning everything after it. If
     *      `needle` does not occur in `self`, `self` is set to the empty slice,
     *      and the entirety of `self` is returned.
     * @param self The slice to split.
     * @param needle The text to search for in `self`.
     * @return The part of `self` after the last occurrence of `delim`.
     */
    function rsplit(slice self, slice needle) internal pure returns (slice token) {
        rsplit(self, needle, token);
    }

    /*
     * @dev Counts the number of nonoverlapping occurrences of `needle` in `self`.
     * @param self The slice to search.
     * @param needle The text to search for in `self`.
     * @return The number of occurrences of `needle` found in `self`.
     */
    function count(slice self, slice needle) internal pure returns (uint cnt) {
        uint ptr = findPtr(self._len, self._ptr, needle._len, needle._ptr) + needle._len;
        while (ptr <= self._ptr + self._len) {
            cnt++;
            ptr = findPtr(self._len - (ptr - self._ptr), ptr, needle._len, needle._ptr) + needle._len;
        }
    }

    /*
     * @dev Returns True if `self` contains `needle`.
     * @param self The slice to search.
     * @param needle The text to search for in `self`.
     * @return True if `needle` is found in `self`, false otherwise.
     */
    function contains(slice self, slice needle) internal pure returns (bool) {
        return rfindPtr(self._len, self._ptr, needle._len, needle._ptr) != self._ptr;
    }

    /*
     * @dev Returns a newly allocated string containing the concatenation of
     *      `self` and `other`.
     * @param self The first slice to concatenate.
     * @param other The second slice to concatenate.
     * @return The concatenation of the two strings.
     */
    function concat(slice self, slice other) internal pure returns (string) {
        string memory ret = new string(self._len + other._len);
        uint retptr;
        assembly { retptr := add(ret, 32) }
        memcpy(retptr, self._ptr, self._len);
        memcpy(retptr + self._len, other._ptr, other._len);
        return ret;
    }

    // added by
    function concatS(slice self, slice other) internal pure returns (slice) {
        string memory ret = new string(self._len + other._len);
        uint retptr;
        assembly { retptr := add(ret, 32) }
        memcpy(retptr, self._ptr, self._len);
        memcpy(retptr + self._len, other._ptr, other._len);
        return slice(self._len + other._len, retptr);
    }

    // added by
    function concatA(slice self, slice[] parts) internal pure returns (string) {
        if (parts.length == 0)
            return "";

        uint length = self._len;
    uint i;
        for(i = 0; i < parts.length; i++)
            length += parts[i]._len;

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

    /*
     * @dev Joins an array of slices, using `self` as a delimiter, returning a
     *      newly allocated string.
     * @param self The delimiter to use.
     * @param parts A list of slices to join.
     * @return A newly allocated string containing all the slices in `parts`,
     *         joined with `self`.
     */
    function join(slice self, slice[] parts) internal pure returns (string) {
        if (parts.length == 0)
            return "";

        uint length = self._len * (parts.length - 1);
    uint i;
        for(i = 0; i < parts.length; i++)
            length += parts[i]._len;

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
        for(uint i = 0; i < len; i++){
            b[i] = data[i + start];
        }
        return b;
    }

    function bytesToBytes32(bytes memory source) internal pure returns (bytes32 result) {
        assembly {
            result := mload(add(source, 32))
        }
    }

    function bytes32ToString(bytes32 x) internal pure returns (string) {
        bytes memory bytesString = new bytes(32);
        uint charCount = 0;
    uint j;
        for (j = 0; j < 32; j++) {
            byte char = byte(bytes32(uint(x) * 2 ** (8 * j)));
            if (char != 0) {
                bytesString[charCount] = char;
                charCount++;
            }
        }
        bytes memory bytesStringTrimmed = new bytes(charCount);
        for (j = 0; j < charCount; j++) {
            bytesStringTrimmed[j] = bytesString[j];
        }

        return string(bytesStringTrimmed);
    }

    function bytes20ToString(bytes20 x) internal pure returns (string) {
        bytes memory bytesString = new bytes(20);
        uint charCount = 0;
        for (uint j = 0; j < 20; j++) {
            byte char = byte(bytes20(uint(x) * 2 ** (8 * j)));
            if (char != 0) {
                bytesString[charCount] = char;
                charCount++;
            }
        }
        bytes memory bytesStringTrimmed = new bytes(charCount);
        for (j = 0; j < charCount; j++) {
            bytesStringTrimmed[j] = bytesString[j];
        }

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
        //bytes memory bbs = bytes(bs);
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
        //bytes memory bbs = bytes(bs);
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
        uint j = i;
        uint length;
        while (j != 0){
            length++;
            j /= 10;
        }
        bytes memory bstr = new bytes(length);
        uint k = length - 1;
        while (i != 0){
            bstr[k--] = byte(uint8(48) + uint8(i % 10));
            i /= 10;
        }
        return string(bstr);
    }

    function bytes32ToBytes(bytes32 data) internal pure returns (bytes) {
        bytes memory result = new bytes(32);
        assembly {
            mstore(add(result, 32), data)
        }
        return result;
    }

    function stringToBytes32(string memory source) internal pure returns (bytes32 result) {
        assembly {
            result := mload(add(source, 32))
        }
    }

    function stringToBytes20(string memory source) internal pure returns (bytes20 result) {
        assembly {
            result := mload(add(source, 20))
        }
    }

    function bytes32ArrayToString(bytes32[] data)  internal pure returns (string) {
        bytes memory bytesString = new bytes(data.length * 32);
        uint urlLength;
        for (uint i = 0; i< data.length; i++) {
            for (uint j = 0; j < 32; j++) {
                byte char = byte(bytes32(uint(data[i]) * 2 ** (8 * j)));
                if (char != 0) {
                    bytesString[urlLength] = char;
                    urlLength += 1;
                }
            }
        }
        bytes memory bytesStringTrimmed = new bytes(urlLength);
        for (i = 0; i < urlLength; i++) {
            bytesStringTrimmed[i] = bytesString[i];
        }
        return string(bytesStringTrimmed);
    }

    function stringToUint(string s) internal pure returns (uint) {
        bytes memory b = bytes(s);
        uint result = 0;
        for (uint i = 0; i < b.length; i++) { // c = b[i] was not needed
            if (b[i] >= 48 && b[i] <= 57) {
                result = result * 10 + (uint(b[i]) - 48); // bytes and int are not compatible with the operator -.
            }
        }
        return result; // this was missing
    }

    function compare_string(string a, string b) internal returns (bool) {
        if (bytes(a).length != bytes(b).length) {
            return false;
        } else {
            return keccak256(a) == keccak256(b);
        }
    }
}

/*************************************************************************************************************************

区块链小程序和区块链浏览器可以展示每条交易的内容(包括txhash、所在区块高度、交易签名等)，若想展示本交易的业务逻辑，则需要按照固定的数据格式将
需要展示的业务逻辑按照json的形式写在交易的receipt log中。其中json的格式如下：


{
    "__type__":"miniappinfo",
    "prevHash":"98ec26e76b33c7814990c3263f35ff500e51deecb6c7f64913b9ae2f76bf7119",
    "nextHash":"",
    "lists":[
        {
            "name":"艺术品表",
            "value":"artId",
            "type":"text"
        },
        {
            "name":"货主",
            "value":"刘备",
            "type":"text"
        },
        {
            "name":"年代",
            "value":"220-10-01",
            "type":"text"
        },
        {
            "name":"艺术品名",
            "value":"玉玺",
            "type":"text"
        },
        {
            "name":"价格",
            "value":"100000000",
            "type":"text"
        },
        {
            "name":"来源信息",
            "value":"交易",
            "type":"text"
        },
        {
            "name":"尺寸",
            "value":"10cm*10cm",
            "type":"text"
        },
        {
            "name":"位置",
            "value":"交易",
            "type":"text"
        }
    ]
}

其中
__type__:minappinfo是小程序识别解析业务数据的标记
preHash是指与本交易相关的上一条交易txhash，若此值被设定，则可以在小程序中左右翻页
nextHash预留字段，目前无用
list：具体业务逻辑数据展示，list的类型为数组，每个元素必须按照如下的格式

{
    "name":"",
    "value":"",
    "type":"text"
}

其中name是业务逻辑字段名，value为对应的数值，type支持text


在区块链中，每个block中包含一些交易，此类交易按照节点的排序分布在block中。交易间不存在关联关系。而实际业务中，有时需要追溯相关数据的历史交易，
此时可以在交易的receipt log中记录上一个交易的txhash，这样就可以实现相关交易的关联。

如下图中blockn中的txn4可以在receipt log中记录上一个相关交易的txhash为tx4，则可以直接根据该交易该txhash查找到tx4交易的内容
若如此做，则可以在区块链小程序业务相关交易的左右翻页

         -----------------------------------------
-----    |   -----       -----        -----      | ------ 
|tx1|    |   |   |       |   |        |   |      | |txn1| 
|---|    |   |---|       |---|        |---|      | |----| 
|tx2|    |   |   |       |   |        |   |      | |txn2| 
|---|<===|===|---|<======|---|<=......|---|<=====|=|----| 
|tx3|    |   |   |       |   |        |   |      | |txn3| 
|---|    |   |---|       |---|        |---|      | |----| 
|tx4|<----   |   |       |   |        |   |      --|txn4| 
-----        -----       -----        -----        ------ 
block1       block2      block3       blockn-1      blockn

**************************************************************************************************************************/

contract ArtManager {

    event LOG_STRING(string);
    using strings for *;
    using utils for *;
    struct Art{
        string artId;
        string date;
        string owner;
        string artName;
        string price;
        string sellway;
        string artSize;
        string location;
        string preTxHash;
    }

    mapping (string => Art) arts;

    function setJsonKV(string json,string key,string value,bool isEnd) private returns (string){
        json = json.toSlice().concat("\"".toSlice());
        json = json.toSlice().concat(key.toSlice());
        json = json.toSlice().concat("\":\"".toSlice());
        json = json.toSlice().concat(value.toSlice());
        json = json.toSlice().concat("\"".toSlice());
        if(!isEnd){
            json = json.toSlice().concat(",".toSlice());
        }
        return json;
    }

    function setItem(string json,string key,string value,string itype,bool isEnd)private returns (string){
        json = json.toSlice().concat("{".toSlice());
        json = setJsonKV(json,"name",key,false);
        json = setJsonKV(json,"value",value,false);
        json = setJsonKV(json,"type",itype,true);
        json = json.toSlice().concat("}".toSlice());
        if(!isEnd){
            json = json.toSlice().concat(",".toSlice());
        }
        return json;
    }

    function tableLog(string artId) private{
        string memory json = "{";
        Art storage art = arts[artId];
        if(art.artId.compare_string("")){
            return;
        }
        json = setJsonKV(json,"__type__","miniappinfo",false);
        json = setJsonKV(json,"prevHash",art.preTxHash,false);
        json = setJsonKV(json,"nextHash","",false);
        json = json.toSlice().concat("\"lists\":[".toSlice());
        json = setItem(json,"艺术品表",art.artId,"text",false);
        json = setItem(json,"货主",art.owner,"text",false);
        json = setItem(json,"年代",art.date,"text",false);
        json = setItem(json,"艺术品名",art.artName,"text",false);
        json = setItem(json,"价格",art.price,"text",false);
        json = setItem(json,"来源信息",art.sellway,"text",false);
        json = setItem(json,"尺寸",art.artSize,"text",false);
        json = setItem(json,"位置",art.location,"text",true);
        
        json = json.toSlice().concat("]}".toSlice());
        emit LOG_STRING(json);
    }

    /*************************************************************************************************************************
    插入艺术品基本信息
    artId:艺术品Id
    date:艺术品年代
    owner:艺术品拥有者
    artName:艺术品名称
    price:艺术品价格
    sellWay:艺术品获得方式
    artSize：艺术品尺寸
    location：艺术品目前所在地
    *************************************************************************************************************************/

    function insertArt(string artId,string date,string artName,string owner,string price,string sellway,string artSize,string location)public{
        Art memory art;
        art.artId = artId;
        art.date = date;
        art.owner = owner;
        art.price = price;
        art.artName = artName;
        art.sellway = sellway;
        art.artSize = artSize;
        art.location = location;
        art.preTxHash = bytes32(tx.txhash).bytesToHexString();
        arts[artId] = art;
        tableLog(artId);
    }


    /*************************************************************************************************************************
    更新艺术品拥有者信息
    *************************************************************************************************************************/
    function updateArtOwner(string artId,string date,string owner)public{
        Art art = arts[artId];
        if(art.artId.compare_string("")){
            return;
        }
        art.owner = owner;
        art.date = date;
        tableLog(artId);
        art.preTxHash = bytes32(tx.txhash).bytesToHexString();
    }

}

